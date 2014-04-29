package main

import (
	"bufio"
	"fmt"
	golog "log"
	"net/http"
	"os"
	"github.com/percona/cloud-protocol/proto"
	httpclient "github.com/mreiferson/go-httpclient"
	"time"
	"errors"
	"database/sql"
	"encoding/json"
	"regexp"
	"io/ioutil"
)

const (
	//CLOUD_API_HOSTNAME = "https://cloud-api.percona.com"
	CLOUD_API_HOSTNAME = "http://localhost:8000"
)

var (
	ErrApiUnauthorized = errors.New("Unauthorized")
	ErrApiUnknown = errors.New("Unknown")
)

var requiredEntryLinks = []string{"agents", "instances", "download"}

func init() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
}

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
	hostname    string
	agentUuid   string
	client      *http.Client
	entryLinks  map[string]string
	agentLinks  map[string]string
}

func NewInstaller() *Installer {
	transport := &httpclient.Transport{
		ConnectTimeout:        1*time.Second,
		RequestTimeout:        10*time.Second,
		ResponseHeaderTimeout: 5*time.Second,
	}

	installer := &Installer{
		mysqlUser: "root",
		mysqlPass: "",
		mysqlHost: "localhost",
		mysqlPort: "3306",
		mysqlSocket: "",
		client: &http.Client{Transport: transport},
	}

	return installer
}

func main() {
	installer := NewInstaller()

	// Server
	err := installer.GetHostname()
	if err != nil {
		golog.Fatalf("Unable to get hostname: %s", err)
	}

	// Api key
	for {
		// Ask user for api key
		err := installer.GetApiKey()
		if err != nil {
			golog.Fatalf("Unable to get API key: %s", err)
		}

		// Check if api key is correct
		err := installer.VerifyApiKey()
		if err == ErrApiUnauthorized {
			fmt.Printf("Unauthorized, check if API key is correct and try again")
			continue
		} else if err != nil {
			fmt.Printf("Unable to verify API key: %s. Contact Percona Support if this error persists.", err)
			continue
		}

		break // success
	}

	// Api entry Links
	err = installer.GetApiLinks()
	if err != nil {
		golog.Fatalf("Unable to get API entry links: %s", err)
	}
	err = installer.VerifyApiLinks()
	if err != nil {
		golog.Fatalf("Bad API entry links: %s", err)
	}

	// Mysql connection details
	for {
		// Ask user for api key
		err := installer.GetMysqlDsn()
		if err != nil {
			golog.Fatalf("An exception occured: %s", err)
		}

		// Check if api key is correct
		err := installer.VerifyMysqlDsn()
		if err != nil {
			fmt.Printf("Unable to verify mysql dsn: %s", err)
			continue
		}

		break // success
	}

	// Create agent
	err = installer.CreateAgent()
	if err != nil {
		golog.Fatalf("Unable to create agent: %s", err)
	}

	installer.Close()
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
			"mm":    "{ type: \"mm_inserted\"}", // @todo
		},
		Versions: map[string]string{
			"PerconaAgent": "1.0.0", // @todo
			"MySQL":        "5.5.6", // @todo
		},
	}

	data, err := json.Marshal(agentData)

	req, err := http.NewRequest("POST", i.entryLinks["agents"], data)
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

	if err := json.Unmarshal(data, i.entryLinks); err != nil {
		return errors.New(fmt.Sprintf("Unable to unmarshal data: %s", err))
	}

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
	fmt.Print("Please provide mysql connection details")

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
		dsn = fmt.Sprintf("%s:%s@unix(%s)", i.mysqlUser, i.mysqlPass, i.mysqlSocket)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)", i.mysqlUser, i.mysqlPass, i.mysqlHost, i.mysqlPort)
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
	i.client.Transport.Close()
}

func AskUserWitDefaultAnswer(question string, defaultAnswer string) (answer string, err error) {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf(question + "[default: %s]: ", defaultAnswer)
	scanner.Scan()
	if err := scanner.Err(); err != nil {
		return "", err
	}
	answer = scanner.Text()
	if answer == "" {
		answer = defaultAnswer
	}

	return answer, nil
}

func AskUser(question string) (answer string, err error) {
	scanner := bufio.NewScanner(os.Stdin)

	for answer == "" {
		fmt.Print(question + ": ")
		scanner.Scan()
		if err := scanner.Err(); err != nil {
			return "", err
		}
		answer = scanner.Text()
	}

	return answer, nil
}

