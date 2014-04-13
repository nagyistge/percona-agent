package pct

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type APIConnector interface {
	Connect(hostname, apiKey, agentUuid string) error
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
	agentLinks map[string]string
	mux        *sync.RWMutex
}

func NewAPI() *API {
	hostname, _ := os.Hostname()
	a := &API{
		origin:     "http://" + hostname,
		agentLinks: make(map[string]string),
		mux:        new(sync.RWMutex),
	}
	return a
}

func PingAPI(hostname, apiKey string) (bool, *http.Response) {
	schema := "https://"
	if strings.HasPrefix(hostname, "localhost") || strings.HasPrefix(hostname, "127.0.0.1") {
		schema = "http://"
	}
	url := schema + hostname + "/ping"

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Ping %s error: http.NewRequest: %s", url, err)
		return false, nil
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

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
	entryLinks, err := a.getLinks(schema + hostname)
	if err != nil {
		return err
	}
	if err := a.checkLinks(entryLinks, "agents"); err != nil {
		return err
	}

	// Get agent links: <API hostname>/agents/
	agentLinks, err := a.getLinks(entryLinks["agents"] + "/" + agentUuid)
	if err != nil {
		return err
	}
	if err := a.checkLinks(agentLinks, "cmd", "log", "data"); err != nil {
		return err
	}

	// Success: API responds with the links we need.
	a.mux.Lock()
	defer a.mux.Unlock()
	a.hostname = hostname
	a.apiKey = apiKey
	a.agentUuid = agentUuid
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

func (a *API) getLinks(url string) (map[string]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Percona-API-Key", a.apiKey)

	// todo: timeout
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s error: client.Do: %s", url, err)
	}

	// todo: timeout
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("GET %s error: ioutil.ReadAll: %s", url, err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Error %d from %s\n", resp.StatusCode, url)
	} else if len(body) == 0 {
		return nil, fmt.Errorf("OK response from ", url, "but no content")
	}

	links := make(map[string]string)
	if err := json.Unmarshal(body, links); err != nil {
		return nil, fmt.Errorf("GET %s error: json.Unmarshal: %s: %s", url, err, string(body))
	}

	return links, nil
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
