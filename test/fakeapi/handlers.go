package fakeapi

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"net/http"
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

func (f *FakeApi) AppendInstancesServer(serverInstance *proto.ServerInstance) {
	f.Append("/instances/server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/server/%d", f.URL(), serverInstance.Id))
		w.WriteHeader(http.StatusCreated)
	})
}
func (f *FakeApi) AppendInstancesServerId(serverInstance *proto.ServerInstance) {
	f.Append(fmt.Sprintf("/instances/server/%d", serverInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&serverInstance)
		w.Write(data)
	})
}
func (f *FakeApi) AppendInstancesMysql(mysqlInstance *proto.MySQLInstance) {
	f.Append("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", f.URL(), mysqlInstance.Id))
		w.WriteHeader(http.StatusCreated)
	})
}
func (f *FakeApi) AppendInstancesMysqlId(mysqlInstance *proto.MySQLInstance) {
	f.Append(fmt.Sprintf("/instances/mysql/%d", mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&mysqlInstance)
		w.Write(data)
	})
}
func (f *FakeApi) AppendConfigsMmDefaultServer() {
	f.Append("/configs/mm/default-server", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "server", "InstanceId": 0, "Collect": 10, "Report": 60 }`))
	})
}
func (f *FakeApi) AppendConfigsMmDefaultMysql() {
	f.Append("/configs/mm/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Collect": 1, "Report": 60, "Status": {}, "UserStats": false }`))
	})
}
func (f *FakeApi) AppendSysconfigDefaultMysql() {
	f.Append("/configs/sysconfig/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Report": 3600 }`))
	})
}
func (f *FakeApi) AppendAgents(agent *proto.Agent) {
	f.Append("/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/agents/%s", f.URL(), agent.Uuid))
		w.WriteHeader(http.StatusCreated)
	})
}
func (f *FakeApi) AppendAgentsUuid(agent *proto.Agent) {
	f.Append(fmt.Sprintf("/agents/%s", agent.Uuid), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&agent)
		w.Write(data)
	})
}
