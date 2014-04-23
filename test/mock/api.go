package mock

import (
	"net/http"
)

type API struct {
	origin    string
	hostname  string
	apiKey    string
	agentUuid string
	links     map[string]string
}

func NewAPI(origin, hostname, apiKey, agentUuid string, links map[string]string) *API {
	a := &API{
		origin:    origin,
		hostname:  hostname,
		apiKey:    apiKey,
		agentUuid: agentUuid,
		links:     links,
	}
	return a
}

func PingAPI(hostname, apiKey string) (bool, *http.Response) {
	return true, nil
}

func (a *API) Connect(hostname, apiKey, agentUuid string) error {
	a.hostname = hostname
	a.apiKey = apiKey
	a.agentUuid = agentUuid
	return nil
}

func (a *API) AgentLink(resource string) string {
	return a.links[resource]
}

func (a *API) EntryLink(resource string) string {
	return a.links[resource]
}

func (a *API) Origin() string {
	return a.origin
}

func (a *API) Hostname() string {
	return a.hostname
}

func (a *API) ApiKey() string {
	return a.apiKey
}

func (a *API) AgentUuid() string {
	return a.agentUuid
}

func (a *API) Get(url string) (int, []byte, error) {
	return 200, nil, nil
}
