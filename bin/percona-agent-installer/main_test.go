/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

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

package main_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/test/cmdtest"
	. "launchpad.net/gocheck"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	basedir string
}

var _ = Suite(&MainTestSuite{
	basedir: "/tmp/percona-agent-installer-test",
})

func (s *MainTestSuite) SetUpSuite(t *C) {
	cmd := exec.Command("go", "build", "-o", "bin/percona-agent-installer/installer", "github.com/percona/percona-agent/bin/percona-agent-installer")
	err := cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to build installer: %s", err))
}

// --------------------------------------------------------------------------
func (s *MainTestSuite) TestWrongApiKey(t *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()
	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"-basedir="+s.basedir,
		"-api-host="+ts.URL,
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	apiKey := "WrongApiKey"
	t.Assert(cmdTest.ReadLine(), Equals, "API key: ")
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "Sorry, there's an API problem (status code 500). Please try to install again. If the problem continues, contact Percona.\n")

	t.Assert(cmdTest.ReadLine(), Equals, "Try again? (Y): ")
	cmdTest.Write("N\n")
	t.Check(cmdTest.ReadLine(), Equals, "Failed to verify API key\n")
	t.Check(cmdTest.ReadLine(), Equals, "Install failed\n")

	t.Assert(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, ErrorMatches, "exit status 1")
}

func (s *MainTestSuite) TestDefaultInstall(t *C) {
	serverInstance := &proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	mysqlInstance := &proto.MySQLInstance{
		Id:       10,
		Hostname: "localhost",
		DSN:      "",
	}
	agentUuid := "0001"
	agent := &proto.Agent{
		Uuid:     agentUuid,
		Hostname: "host1",
		Alias:    "master-db",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + agentUuid + "/log",
		},
	}

	// Create fake http server
	sm := NewServeMuxTest()
	ts := httptest.NewServer(sm)
	defer ts.Close()

	// Register required mock http handlers
	sm.appendPing()
	sm.appendInstancesServer(ts.URL, serverInstance)
	sm.appendInstancesServerId(serverInstance)
	sm.appendInstancesMysql(ts.URL, mysqlInstance)
	sm.appendInstancesMysqlId(mysqlInstance)
	sm.appendConfigsMmDefaultServer()
	sm.appendConfigsMmDefaultMysql()
	sm.appendSysconfigDefaultMysql()
	sm.appendAgents(ts.URL, agent)
	sm.appendAgentsUuid(agent)

	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"-basedir="+s.basedir,
		"-api-host="+ts.URL,
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", serverInstance.Hostname, serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "Testing MySQL connection root:...@unix(/var/run/mysqld/mysqld.sock)...\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")

	re := regexp.MustCompile("0x[^@]+")
	lineWithoutPassword := re.ReplaceAllString(cmdTest.ReadLine(), "<pass>") // @todo read pass hash from db
	t.Check(lineWithoutPassword, Equals, "Agent MySQL user: percona-agent:<pass>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, mysqlInstance.Hostname, mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestDefaultInstallWithFlagOldPasswordsTrue(t *C) {
	serverInstance := &proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	mysqlInstance := &proto.MySQLInstance{
		Id:       10,
		Hostname: "localhost",
		DSN:      "",
	}
	agentUuid := "0001"
	agent := &proto.Agent{
		Uuid:     agentUuid,
		Hostname: "host1",
		Alias:    "master-db",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + agentUuid + "/log",
		},
	}

	// Create fake http server
	sm := NewServeMuxTest()
	ts := httptest.NewServer(sm)
	defer ts.Close()

	// Register required mock http handlers
	sm.appendPing()
	sm.appendInstancesServer(ts.URL, serverInstance)
	sm.appendInstancesServerId(serverInstance)
	sm.appendInstancesMysql(ts.URL, mysqlInstance)
	sm.appendInstancesMysqlId(mysqlInstance)
	sm.appendConfigsMmDefaultServer()
	sm.appendConfigsMmDefaultMysql()
	sm.appendSysconfigDefaultMysql()
	sm.appendAgents(ts.URL, agent)
	sm.appendAgentsUuid(agent)

	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"-basedir="+s.basedir,
		"-api-host="+ts.URL,
		"-old-passwords=true",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", serverInstance.Hostname, serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "Testing MySQL connection root:...@unix(/var/run/mysqld/mysqld.sock)...\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")

	re := regexp.MustCompile("0x[^@]+")
	lineWithoutPassword := re.ReplaceAllString(cmdTest.ReadLine(), "<pass>") // @todo read pass hash from db
	// Flag -old-passwords=true should add &allowOldPasswords=true to DSN
	t.Check(lineWithoutPassword, Equals, "Agent MySQL user: percona-agent:<pass>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true&allowOldPasswords=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, mysqlInstance.Hostname, mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestDefaultInstallWithFlagApiKey(t *C) {
	serverInstance := &proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	mysqlInstance := &proto.MySQLInstance{
		Id:       10,
		Hostname: "localhost",
		DSN:      "",
	}
	agentUuid := "0001"
	agent := &proto.Agent{
		Uuid:     agentUuid,
		Hostname: "host1",
		Alias:    "master-db",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + agentUuid + "/log",
		},
	}

	// Create fake http server
	sm := NewServeMuxTest()
	ts := httptest.NewServer(sm)
	defer ts.Close()

	// Register required mock http handlers
	sm.appendPing()
	sm.appendInstancesServer(ts.URL, serverInstance)
	sm.appendInstancesServerId(serverInstance)
	sm.appendInstancesMysql(ts.URL, mysqlInstance)
	sm.appendInstancesMysqlId(mysqlInstance)
	sm.appendConfigsMmDefaultServer()
	sm.appendConfigsMmDefaultMysql()
	sm.appendSysconfigDefaultMysql()
	sm.appendAgents(ts.URL, agent)
	sm.appendAgentsUuid(agent)

	apiKey := "00000000000000000000000000000001"
	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"-basedir="+s.basedir,
		"-api-host="+ts.URL,
		"-api-key="+apiKey, // We are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	// Because of -api-key flag user don't provides it by hand
	//t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	//cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", serverInstance.Hostname, serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "Testing MySQL connection root:...@unix(/var/run/mysqld/mysqld.sock)...\n")

	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")

	re := regexp.MustCompile("0x[^@]+")
	lineWithoutPassword := re.ReplaceAllString(cmdTest.ReadLine(), "<pass>") // @todo read pass hash from db
	t.Check(lineWithoutPassword, Equals, "Agent MySQL user: percona-agent:<pass>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, mysqlInstance.Hostname, mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestDefaultInstallWithFlagMysqlFalse(t *C) {
	serverInstance := &proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	agentUuid := "0001"
	agent := &proto.Agent{
		Uuid:     agentUuid,
		Hostname: "host1",
		Alias:    "master-db",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + agentUuid + "/log",
		},
	}

	// Create fake http server
	sm := NewServeMuxTest()
	ts := httptest.NewServer(sm)
	defer ts.Close()

	// Register required mock http handlers
	sm.appendPing()
	sm.appendInstancesServer(ts.URL, serverInstance)
	sm.appendInstancesServerId(serverInstance)
	// Flag -mysql=false implies that there should be not below communication
	//sm.appendInstancesMysql(ts.URL, mysqlInstance)
	//sm.appendInstancesMysqlId(mysqlInstance)
	//sm.appendConfigsMmDefaultMysql()
	//sm.appendSysconfigDefaultMysql()
	sm.appendConfigsMmDefaultServer()
	sm.appendAgents(ts.URL, agent)
	sm.appendAgentsUuid(agent)

	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"-mysql=false", // We are testing this flag
		"-basedir="+s.basedir,
		"-api-host="+ts.URL,
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", serverInstance.Hostname, serverInstance.Id))

	// Flag -mysql=false implies below
	t.Check(cmdTest.ReadLine(), Equals, "Not creating MySQL instance (-create-mysql-instance=false)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Not starting MySQL services (-start-mysql-services=false)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

type ServeMuxTest struct {
	*http.ServeMux
}

func NewServeMuxTest() *ServeMuxTest {
	return &ServeMuxTest{
		http.NewServeMux(),
	}
}

func (sm *ServeMuxTest) appendPing() {
	sm.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
		}
		w.WriteHeader(600)
	})
}

func (sm *ServeMuxTest) appendInstancesServer(url string, serverInstance *proto.ServerInstance) {
	sm.HandleFunc("/instances/server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/server/%d", url, serverInstance.Id))
		w.WriteHeader(http.StatusCreated)
	})
}
func (sm *ServeMuxTest) appendInstancesServerId(serverInstance *proto.ServerInstance) {
	sm.HandleFunc(fmt.Sprintf("/instances/server/%d", serverInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&serverInstance)
		w.Write(data)
	})
}
func (sm *ServeMuxTest) appendInstancesMysql(url string, mysqlInstance *proto.MySQLInstance) {
	sm.HandleFunc("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", url, mysqlInstance.Id))
		w.WriteHeader(http.StatusCreated)
	})
}
func (sm *ServeMuxTest) appendInstancesMysqlId(mysqlInstance *proto.MySQLInstance) {
	sm.HandleFunc(fmt.Sprintf("/instances/mysql/%d", mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&mysqlInstance)
		w.Write(data)
	})
}
func (sm *ServeMuxTest) appendConfigsMmDefaultServer() {
	sm.HandleFunc("/configs/mm/default-server", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "server", "InstanceId": 0, "Collect": 10, "Report": 60 }`))
	})
}
func (sm *ServeMuxTest) appendConfigsMmDefaultMysql() {
	sm.HandleFunc("/configs/mm/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Collect": 1, "Report": 60, "Status": {}, "UserStats": false }`))
	})
}
func (sm *ServeMuxTest) appendSysconfigDefaultMysql() {
	sm.HandleFunc("/configs/sysconfig/default-mysql", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Report": 3600 }`))
	})
}
func (sm *ServeMuxTest) appendAgents(url string, agent *proto.Agent) {
	sm.HandleFunc("/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/agents/%s", url, agent.Uuid))
		w.WriteHeader(http.StatusCreated)
	})
}
func (sm *ServeMuxTest) appendAgentsUuid(agent *proto.Agent) {
	sm.HandleFunc(fmt.Sprintf("/agents/%s", agent.Uuid), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&agent)
		w.Write(data)
	})
}
