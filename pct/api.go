package pct

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

var requiredEntryLinks = []string{"agents", "instances", "download"}
var requiredAgentLinks = []string{"cmd", "log", "data"}

type APIConnector interface {
	Connect(hostname, apiKey, agentUuid string) error
	Get(url string) (int, []byte, error)
	EntryLink(resource string) string
	AgentLink(resource string) string
	Origin() string
	Hostname() string
	ApiKey() string
	AgentUuid() string
}

type API struct {
	origin     string
	hostname   string
	apiKey     string
	agentUuid  string
	entryLinks map[string]string
	agentLinks map[string]string
	mux        *sync.RWMutex
	client     *http.Client
}

func NewAPI() *API {
	hostname, _ := os.Hostname()
	a := &API{
		origin:     "http://" + hostname,
		agentLinks: make(map[string]string),
		mux:        new(sync.RWMutex),
		client:     &http.Client{},
	}
	return a
}

func PingAPI(hostname, apiKey string) (bool, *http.Response) {
	schema := "https://"
	if strings.HasPrefix(hostname, "localhost") || strings.HasPrefix(hostname, "127.0.0.1") {
		schema = "http://"
	}
	url := schema + hostname + "/ping"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Ping %s error: http.NewRequest: %s", url, err)
		return false, nil
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Ping %s error: client.Do: %s", url, err)
		return false, resp
	}
	_, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("Ping %s error: ioutil.ReadAll: %s", url, err)
		return false, resp
	}
	if resp.StatusCode != 200 {
		return false, resp
	}

	return true, resp // success
}

func (a *API) Connect(hostname, apiKey, agentUuid string) error {
	schema := "https://"
	if strings.HasPrefix(hostname, "localhost") || strings.HasPrefix(hostname, "127.0.0.1") {
		schema = "http://"
	}

	// Get entry links: GET <API hostname>/
	entryLinks, err := a.getLinks(apiKey, schema+hostname)
	if err != nil {
		return err
	}
	if err := a.checkLinks(entryLinks, requiredEntryLinks...); err != nil {
		return err
	}

	// Get agent links: <API hostname>/agents/
	agentLinks, err := a.getLinks(apiKey, entryLinks["agents"]+"/"+agentUuid)
	if err != nil {
		return err
	}
	if err := a.checkLinks(agentLinks, requiredAgentLinks...); err != nil {
		return err
	}

	// Success: API responds with the links we need.
	a.mux.Lock()
	defer a.mux.Unlock()
	a.hostname = hostname
	a.apiKey = apiKey
	a.agentUuid = agentUuid
	a.entryLinks = entryLinks
	a.agentLinks = agentLinks
	return nil
}

func (a *API) checkLinks(links map[string]string, req ...string) error {
	for _, link := range req {
		logLink, exist := links[link]
		if !exist || logLink == "" {
			return errors.New("Missing " + link + " link")
		}
	}
	return nil
}

func (a *API) getLinks(apiKey, url string) (map[string]string, error) {
	code, data, err := a.get(apiKey, url)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("Error %d from %s\n", code, url)
	} else if len(data) == 0 {
		return nil, fmt.Errorf("OK response from ", url, "but no content")
	}

	links := &proto.Links{}
	if err := json.Unmarshal(data, links); err != nil {
		return nil, fmt.Errorf("GET %s error: json.Unmarshal: %s: %s", url, err, string(data))
	}

	return links.Links, nil
}

func (a *API) Get(url string) (int, []byte, error) {
	if a.apiKey == "" {
		return 0, nil, errors.New("API key not set")
	}
	return a.get(a.apiKey, url)
}

func (a *API) get(apiKey, url string) (int, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	// todo: timeout
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s error: client.Do: %s", url, err)
	}
	defer resp.Body.Close()

	var data []byte
	if resp.Header.Get("Content-Type") == "application/x-gzip" {
		buf := new(bytes.Buffer)
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return 0, nil, err
		}
		if _, err := io.Copy(buf, gz); err != nil {
			return resp.StatusCode, nil, err
		}
		data = buf.Bytes()
	} else {
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, nil, fmt.Errorf("GET %s error: ioutil.ReadAll: %s", url, err)
		}
	}

	return resp.StatusCode, data, nil
}

func (a *API) EntryLink(resource string) string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.entryLinks[resource]
}

func (a *API) AgentLink(resource string) string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.agentLinks[resource]
}

func (a *API) Origin() string {
	// No lock because origin doesn't change.
	return a.origin
}

func (a *API) Hostname() string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.hostname
}

func (a *API) ApiKey() string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.apiKey
}

func (a *API) AgentUuid() string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	return a.agentUuid
}
