package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	httpclient "github.com/mreiferson/go-httpclient"
	"github.com/percona/cloud-protocol/proto"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"time"
)

var (
	ErrApiUnauthorized = errors.New("Unauthorized")
	ErrApiUnknown      = errors.New("Unknown")
)

var requiredEntryLinks = []string{"agents", "instances", "download"}

type Installer struct {
	// Api
	apiHostname string
	apiKey      string
	// Mysql instance
	mysqlUser   string
	mysqlPass   string
	mysqlHost   string
	mysqlPort   string
	mysqlSocket string
	// Server
	hostname   string
	agentUuid  string
	client     *http.Client
	transport  *httpclient.Transport
	entryLinks map[string]string
	agentLinks map[string]string
}

func NewInstaller() *Installer {
	transport := &httpclient.Transport{
		ConnectTimeout:        1 * time.Second,
		RequestTimeout:        10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	installer := &Installer{
		mysqlUser:   "root",
		mysqlPass:   "",
		mysqlHost:   "localhost",
		mysqlPort:   "3306",
		mysqlSocket: "",
		client:      &http.Client{Transport: transport},
		entryLinks:  make(map[string]string),
		transport:   transport,
	}

	return installer
}

func (i *Installer) GetHostname() (err error) {
	i.hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	return nil
}

func (i *Installer) CreateAgent() (err error) {
	agentData := proto.AgentData{
		Hostname: i.hostname,
		Configs: map[string]string{
			"agent": "{ type: \"agent_inserted\"}", // @todo
			"mm":    "{ type: \"mm_inserted\"}",    // @todo
		},
		Versions: map[string]string{
			"PerconaAgent": "1.0.0", // @todo
			"MySQL":        "5.5.6", // @todo
		},
	}

	data, err := json.Marshal(agentData)

	req, err := http.NewRequest("POST", i.entryLinks["agents"], bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Add("X-Percona-API-Key", i.apiKey)

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrApiUnauthorized
	} else if resp.StatusCode != http.StatusCreated {
		return errors.New(fmt.Sprintf("Incorrect Api response: code=%d, status=%s", resp.StatusCode, resp.Status))
	}

	var validUuid = regexp.MustCompile(`[a-z0-9\-]+$`)
	i.agentUuid = validUuid.FindString(resp.Header.Get("Location"))
	if i.agentUuid == "" {
		return errors.New("No uuid found in the Header Location")
	}

	return nil // success
}

func (i *Installer) GetApiKey() (err error) {
	if i.apiKey == "" {
		i.apiKey, err = AskUser("Please provide API key")
	} else {
		i.apiKey, err = AskUserWitDefaultAnswer("Please provide API key", i.apiKey)
	}
	return err
}

func (i *Installer) VerifyApiKey() (err error) {
	url := CLOUD_API_HOSTNAME + "/ping"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("X-Percona-API-Key", i.apiKey)

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrApiUnauthorized
	} else if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("Incorrect Api response: code=%d, status=%s", resp.StatusCode, resp.Status))
	}

	return nil // success
}

func (i *Installer) GetApiLinks() (err error) {
	url := CLOUD_API_HOSTNAME

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("X-Percona-API-Key", i.apiKey)

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrApiUnauthorized
	} else if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("Incorrect Api response: code=%d, status=%s", resp.StatusCode, resp.Status))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.New(fmt.Sprintf("Error reading data: %s", err))
	}

	links := &proto.Links{}
	if err := json.Unmarshal(data, links); err != nil {
		return errors.New(fmt.Sprintf("Unable to unmarshal data: %s", err))
	}
	i.entryLinks = links.Links

	return nil // success
}

func (i *Installer) VerifyApiLinks() (err error) {
	for _, link := range requiredEntryLinks {
		logLink, exist := i.entryLinks[link]
		if !exist || logLink == "" {
			return errors.New("Missing " + link + " link")
		}
	}

	return nil // success
}

func (i *Installer) GetMysqlDsn() (err error) {
	fmt.Println("Please provide mysql connection details")

	i.mysqlUser, err = AskUserWitDefaultAnswer("User", i.mysqlUser)
	if err != nil {
		return err
	}

	i.mysqlPass, err = AskUserWitDefaultAnswer("Password", i.mysqlPass)
	if err != nil {
		return err
	}

	i.mysqlSocket, err = AskUserWitDefaultAnswer("Socket (leave blank if you want to connect using Hostname and Port)", i.mysqlSocket)
	if err != nil {
		return err
	}

	if i.mysqlSocket == "" {
		i.mysqlHost, err = AskUserWitDefaultAnswer("Hostname", i.mysqlHost)
		if err != nil {
			return err
		}

		i.mysqlPort, err = AskUserWitDefaultAnswer("Port", i.mysqlPort)
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *Installer) VerifyMysqlDsn() (err error) {
	dsn := ""
	if i.mysqlSocket != "" {
		dsn = fmt.Sprintf("%s:%s@unix(%s)/", i.mysqlUser, i.mysqlPass, i.mysqlSocket)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/", i.mysqlUser, i.mysqlPass, i.mysqlHost, i.mysqlPort)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	if err := db.Ping(); err != nil {
		return err
	}

	return nil // success
}

func (i *Installer) Close() {
	// Example on https://github.com/mreiferson/go-httpclient#example
	// says to Transport.Close(), though it does nothing currently
	i.transport.Close()
}

