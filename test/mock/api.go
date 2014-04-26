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
	GetCode   []int
	GetData   [][]byte
	GetError  []error
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
	code := 200
	var data []byte
	var err error
	if len(a.GetCode) > 0 {
		code = a.GetCode[0]
		a.GetCode = a.GetCode[1:len(a.GetCode)]
	}
	if len(a.GetData) > 0 {
		data = a.GetData[0]
		a.GetData = a.GetData[1:len(a.GetData)]
	}
	if len(a.GetError) > 0 {
		err = a.GetError[0]
		a.GetError = a.GetError[1:len(a.GetError)]
	}
	return code, data, err
}
