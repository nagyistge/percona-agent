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

package fakeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mm"
	"github.com/percona/percona-agent/mm/mysql"
	"github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/sysconfig"
	sysconfigMysql "github.com/percona/percona-agent/sysconfig/mysql"
)

const AGENT_INST_PREFIX = "agent"

var (
	ConfigMmDefaultMysql = mysql.Config{
		Config: mm.Config{
			Collect: 1,
			Report:  60,
		},
		UserStats: false,
	}
	ConfigMmDefaultOS = system.Config{
		Config: mm.Config{
			Collect: 1,
			Report:  60,
		},
	}
	ConfigSysconfigDefaultMysql = sysconfigMysql.Config{
		Config: sysconfig.Config{
			Report: 3600,
		},
	}
)

func (f *FakeApi) AppendPing() {
	f.Append("/ping", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
}

func swapHTTPScheme(url, newScheme string) string {
	splittedUrl := strings.Split(url, "://")
	if len(splittedUrl) != 2 {
		return url
	}
	return newScheme + splittedUrl[1]
}

// /instances will be queried more than once
func (f *FakeApi) AppendInstances(getInst *proto.Instance, postInsts []*proto.Instance) {
	f.Append("/instances", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if getInst == nil {
				panic(errors.New("Tried to GET /instances but handler had no data to serve"))
			}
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&getInst)
			w.Write(data)
		case "POST":
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				panic(err)
			}
			var inst *proto.Instance
			err = json.Unmarshal(body, &inst)
			if err != nil {
				panic(err)
			}
			if len(postInsts) == 0 {
				panic(errors.New("Tried to POST /instances but handler don't have UUIDs to return a valid Location"))
			}

			newInst := postInsts[0]
			postInsts = postInsts[1:]
			w.Header().Set("Location", fmt.Sprintf("%s/instances/%s", f.URL(), newInst.UUID))
			// Links metadata only for agent instances
			if inst.Prefix == AGENT_INST_PREFIX {
				ws_url := swapHTTPScheme(f.URL(), WS_SCHEME)
				// Avoid using Set - it canonicalizes header
				w.Header()["X-Percona-Agent-URL-Cmd"] = []string{fmt.Sprintf("%s/instances/%s/cmd", ws_url, newInst.UUID)}
				w.Header()["X-Percona-Agent-URL-Data"] = []string{fmt.Sprintf("%s/instances/%s/data", ws_url, newInst.UUID)}
				w.Header()["X-Percona-Agent-URL-Log"] = []string{fmt.Sprintf("%s/instances/%s/log", ws_url, newInst.UUID)}
				w.Header()["X-Percona-Agent-URL-SystemTree"] = []string{fmt.Sprintf("%s/instances/%s?recursive=true", f.URL(), newInst.ParentUUID)}
				//fmt.Println(fmt.Sprintf("Returned headers: %s", w.Header()))
			}
			w.WriteHeader(http.StatusCreated)
		}

	})
}

func (f *FakeApi) AppendSystemTree(inst *proto.Instance) {
	f.Append(fmt.Sprintf("/instances/%s?recursive=true", inst.UUID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&inst)
		w.Write(data)
	})
}

func (f *FakeApi) AppendInstancesUUID(inst *proto.Instance) {
	f.Append(fmt.Sprintf("/instances/%s", inst.UUID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&inst)
		w.Write(data)
	})
}

func (f *FakeApi) AppendConfigsMmDefaultOS() {
	f.Append("/configs/mm/default-os", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&ConfigMmDefaultOS)
		w.Write(data)
	})
}
func (f *FakeApi) AppendConfigsMmDefaultMysql() {
	f.Append("/configs/mm/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&ConfigMmDefaultMysql)
		w.Write(data)
	})
}
func (f *FakeApi) AppendSysconfigDefaultMysql() {
	f.Append("/configs/sysconfig/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&ConfigSysconfigDefaultMysql)
		w.Write(data)
	})
}
func (f *FakeApi) AppendConfigsQanDefault() {
	f.Append("/configs/qan/default", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "UUID": "0", "Interval": 60}`))
	})
}

//func (f *FakeApi) AppendAgents(agent *proto.Agent) {
//	f.Append("/agents", func(w http.ResponseWriter, r *http.Request) {
//		w.Header().Set("Location", fmt.Sprintf("%s/agents/%s", f.URL(), agent.UUID))
//		w.WriteHeader(http.StatusCreated)
//	})
//}

//func (f *FakeApi) AppendAgentsUuid(agent *proto.Agent) {
//	f.Append(fmt.Sprintf("/agents/%s", agent.UUID), func(w http.ResponseWriter, r *http.Request) {
//		w.WriteHeader(http.StatusOK)
//		data, _ := json.Marshal(&agent)
//		w.Write(data)
//	})
//}
