package mock

import (
	"net/http"
)

type API struct {
	origin     string
	hostname   string
	apiKey     string
	agentUuid  string
	agentLinks map[string]string
}

func NewAPI(origin, hostname, apiKey, agentUuid string, agentLinks map[string]string) *API {
	a := &API{
		origin:     origin,
		hostname:   hostname,
		apiKey:     apiKey,
		agentUuid:  agentUuid,
		agentLinks: agentLinks,
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
	return a.agentLinks[resource]
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
