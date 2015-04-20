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

func New(apiConnector pct.APIConnector, debug bool) *Api {
	return &Api{
		apiConnector: apiConnector,
		debug:        debug,
	}
}

func (a *Api) Init(hostname, apiKey string, headers map[string]string) (code int, err error) {
	return a.apiConnector.Init(hostname, apiKey, headers)
}

// CreateInstance will POST the instance and make sure the request was successful by GET-ing and returning the new
// resource. Metadata associated with the resource will be returned as a string map of string maps, for now only
// metadata for agent resources links are returned when provided by API.
//
// metadata["links"]["self"]        - URL of resource, present in all resources
// metadata["links"]["data"]        - URL for data endpoint
// metadata["links"]["log"]         - URL for log endpoint
// metadata["links"]["cmd"]         - URL for command endpoint
// metadata["links"]["system_tree"] - URL for obtaining System Tree of agent
func (a *Api) CreateInstance(it *proto.Instance) (newIt *proto.Instance, metadata map[string]map[string]string, err error) {
	metadata = make(map[string]map[string]string)
	data, err := json.Marshal(it)
	if err != nil {
		return nil, metadata, err
	}
	url := a.apiConnector.URL("/instances")
	resp, _, err := a.apiConnector.Post(a.apiConnector.ApiKey(), url, data)
	if a.debug {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, metadata, err
	}

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		// agent was created or already exist - either is ok, continue
	} else if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-Percona-Agents-Limit") != "" {
		return nil, metadata, fmt.Errorf(
			"Maximum number of %s agents exceeded.\n"+
				"Go to https://cloud.percona.com/agents and remove unused agents or contact Percona to increase limit.",
			resp.Header.Get("X-Percona-Agents-Limit"),
		)
	} else {
		return nil, metadata, fmt.Errorf("Failed to create %s instance (status code %d)", it.Type, resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, metadata, fmt.Errorf("API did not return location of new %s instance", it.Type)
	}

	if resp.StatusCode == http.StatusConflict {
		resp, _, err := a.apiConnector.Put(a.apiConnector.ApiKey(), uri, data)
		if a.debug {
			log.Printf("resp=%#v\n", resp)
			log.Printf("err=%s\n", err)
		}
		if err != nil {
			return nil, metadata, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, metadata, fmt.Errorf("Failed to update %s instance (status code %d)", it.Type, resp.StatusCode)
		}
	}

	// Collect metadata
	metadata["links"] = make(map[string]string) // initialize links map
	metadata["links"]["self"] = uri             //Location applies to all instances

	// Get the rest of interesting metadata
	// For now we only want some links associated with agent instances
	for header, _ := range resp.Header {
		url := resp.Header.Get(header)
		if url == "" {
			continue
		}
		// Headers are canonicalized
		switch header {
		case "X-Percona-Agent-Url-Cmd":
			metadata["links"]["cmd"] = url
		case "X-Percona-Agent-Url-Data":
			metadata["links"]["data"] = url
		case "X-Percona-Agent-Url-Log":
			metadata["links"]["log"] = url
		case "X-Percona-Agent-Url-Systemtree":
			metadata["links"]["system_tree"] = url
		}
	}

	// GET <api>/instances/:uuid
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), uri)
	if a.debug {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, metadata, err
	}
	if code != http.StatusOK {
		return nil, metadata, fmt.Errorf("Failed to get new instance (status code %d)", code)
	}
	if err := json.Unmarshal(data, &newIt); err != nil {
		return nil, metadata, fmt.Errorf("Failed to parse instance entity: %s", err)
	}
	return newIt, metadata, nil
}

// Gets System Tree from API and deserializes it to proto.Instance
func (a *Api) GetSystemTree(systemTreeURL string) (it *proto.Instance, err error) {
	code, data, err := a.apiConnector.Get(a.apiConnector.ApiKey(), systemTreeURL)
	if a.debug {
		log.Printf("code=%#d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}

	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get System Tree via API (status code %d)", code)
	}
	if err := json.Unmarshal(data, &it); err != nil {
		return nil, err
	}

	return it, nil
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
		Tool:    "mm",
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
		Tool:    "mm",
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
		Tool:    "sysconfig",
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
		Tool:    "qan",
		UUID:    mi.UUID,
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}
