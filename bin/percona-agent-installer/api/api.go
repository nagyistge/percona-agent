/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jagregory/halgo"
	"github.com/percona/cloud-protocol/proto/v2"
	mmMySQL "github.com/percona/percona-agent/mm/mysql"
	mmOS "github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	sysconfigMySQL "github.com/percona/percona-agent/sysconfig/mysql"
)

type Api struct {
	apiConnector pct.APIConnector
	debug        bool
}

type InstanceHAL struct {
	halgo.Links
	proto.Instance
}

func New(apiConnector pct.APIConnector, debug bool) *Api {
	return &Api{
		apiConnector: apiConnector,
		debug:        debug,
	}
}

func (a *Api) Init(hostname, apiKey string, headers map[string]string) (code int, err error) {
	return a.apiConnector.Init(hostname, apiKey, headers)
}

// CreateInstance will POST the instance and if successful it will GET the newly created resource, parsing and
// returning link metadata provided with the resource as a map[string]string.
// links["self"]        - URL of resource, present in all resources
// links["data"]        - URL of data endpoint
// links["log"]         - URL of log endpoint
// links["cmd"]         - URL of command endpoint
func (a *Api) CreateInstance(it *proto.Instance) (newIt *proto.Instance, links map[string]string, err error) {
	data, err := json.Marshal(it)
	if err != nil {
		return nil, nil, err
	}
	url := a.apiConnector.URL("/v3-instances")
	resp, _, err := a.apiConnector.Post(a.apiConnector.ApiKey(), url, data)
	if a.debug {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		// agent was created or already exist - either is ok, continue
	} else if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-Percona-Limit-Err") != "" {
		var err error
		switch it.Prefix {
		case "agent":
			err = fmt.Errorf("Maximum number of %s agents exceeded.\n"+
				"Go to https://cloud.percona.com/ and remove unused agents or contact Percona to increase limit.",
				resp.Header.Get("X-Percona-Limit-Err"))
		case "os":
			err = fmt.Errorf("Maximum number of %s OS instances exceeded.\n"+
				"Go to https://cloud.percona.com/ and remove unused OS instances or contact Percona to increase limit.",
				resp.Header.Get("X-Percona-Limit-Err"))
		default:
			// This should never happen, but we must handle it
			err = fmt.Errorf("Maximum number of %s %s instances exceeded.\n"+
				"Go to https://cloud.percona.com/ and remove unused %s instances or contact Percona to increase limit.",
				it.Type, resp.Header.Get("X-Percona-Limit-Err"))
		}
		return nil, nil, err
	} else {
		return nil, nil, fmt.Errorf("Failed to create %s instance (status code %d)", it.Type, resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, nil, fmt.Errorf("API did not return location of new %s instance", it.Type)
	}

	if resp.StatusCode == http.StatusConflict {
		resp, _, err := a.apiConnector.Put(a.apiConnector.ApiKey(), uri, data)
		if a.debug {
			log.Printf("resp=%#v\n", resp)
			log.Printf("err=%s\n", err)
		}
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("Failed to update %s instance (status code %d)", it.Type, resp.StatusCode)
		}
	}

	// GET <api>/instances/:uuid
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), uri)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, nil, err
	}
	if code != http.StatusOK {
		return nil, nil, fmt.Errorf("Failed to get new instance (status code %d)", code)
	}
	var newInstHAL *InstanceHAL
	if err := json.Unmarshal(data, &newInstHAL); err != nil {
		return nil, nil, fmt.Errorf("Failed to parse instance entity: %s", err)
	}

	/*
	* Collect links
	 */
	links = map[string]string{}
	wantedLinks := []string{"self", "cmd", "data", "log"} // links that may appear in _links section of instance JSON
	for _, rel := range wantedLinks {
		if uri, err := newInstHAL.Links.Href(rel); err == nil {
			links[rel] = uri
		}
	}

	newIt = &newInstHAL.Instance
	return newIt, links, nil
}

// Gets the default OS MM config from API
func (a *Api) GetMmOSConfig(oi *proto.Instance) (*proto.AgentConfig, error) {
	url := a.apiConnector.URL("/configs/mm/default-os")
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), url)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default OS monitor config (%s, status %d)", url, code)
	}
	config := &mmOS.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.UUID = oi.UUID

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		Service: "mm",
		UUID:    oi.UUID,
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

// Gets default MySQL MM config from API
func (a *Api) GetMmMySQLConfig(mi *proto.Instance) (*proto.AgentConfig, error) {
	url := a.apiConnector.URL("/configs/mm/default-mysql")
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), url)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL monitor config (%s, status %d)", url, code)
	}
	config := &mmMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.UUID = mi.UUID

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		Service: "mm",
		UUID:    mi.UUID,
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

// Gets default MySQL SysConfig from API
func (a *Api) GetSysconfigMySQLConfig(mi *proto.Instance) (*proto.AgentConfig, error) {
	url := a.apiConnector.URL("/configs/sysconfig/default-mysql")
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), url)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL sysconfig config (%s, status %d)", url, code)
	}
	config := &sysconfigMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.UUID = mi.UUID

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		Service: "sysconfig",
		UUID:    mi.UUID,
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

// Gets the default QAN config from API
func (a *Api) GetQanConfig(mi *proto.Instance) (*proto.AgentConfig, error) {
	url := a.apiConnector.URL("/configs/qan/default")
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), url)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default Query Analytics config (%s, status %d)", url, code)
	}
	config := &qan.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.UUID = mi.UUID

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		Service: "qan",
		UUID:    mi.UUID,
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}
