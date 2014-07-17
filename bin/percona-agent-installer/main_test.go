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
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"io"
	. "launchpad.net/gocheck"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
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
		"--basedir", s.basedir,
		"--api-host", ts.URL,
	)

	cmdTest := NewCmdTest(cmd)

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

func (s *MainTestSuite) TestBasicInstall(t *C) {
	serverInstance := proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	mysqlInstance := proto.MySQLInstance{
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
	url := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
		} else if r.URL.Path == "/instances/server" {
			w.Header().Set("Location", fmt.Sprintf("%s/instances/server/%d", url, serverInstance.Id))
			w.WriteHeader(http.StatusCreated)
		} else if r.URL.Path == fmt.Sprintf("/instances/server/%d", serverInstance.Id) {
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&serverInstance)
			w.Write(data)
		} else if r.URL.Path == "/instances/mysql" {
			w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", url, mysqlInstance.Id))
			w.WriteHeader(http.StatusCreated)
		} else if r.URL.Path == fmt.Sprintf("/instances/mysql/%d", mysqlInstance.Id) {
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&mysqlInstance)
			w.Write(data)
		} else if r.URL.Path == "/configs/mm/default-server" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{ "Service": "server", "InstanceId": 0, "Collect": 10, "Report": 60 }`))
		} else if r.URL.Path == "/configs/mm/default-mysql" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Collect": 1, "Report": 60, "Status": {}, "UserStats": false }`))
		} else if r.URL.Path == "/configs/sysconfig/default-mysql" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{ "Service": "mysql", "InstanceId": 0, "Report": 3600 }`))
		} else if r.URL.Path == "/agents" {
			w.Header().Set("Location", fmt.Sprintf("%s/agents/%s", url, agent.Uuid))
			w.WriteHeader(http.StatusCreated)
		} else if r.URL.Path == fmt.Sprintf("/agents/%s", agentUuid) {
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&agent)
			w.Write(data)
		}

	}))
	url = ts.URL
	defer ts.Close()
	cmd := exec.Command(
		"./bin/percona-agent-installer/installer",
		"--basedir", s.basedir,
		"--api-host", ts.URL,
	)

	cmdTest := NewCmdTest(cmd)

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

	//t.Check(cmdTest.ReadLine(), Equals, "Agent MySQL user: percona-agent:0xc2080a23f02596996162@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")
	cmdTest.ReadLine() // @todo ^

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, mysqlInstance.Hostname, mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

type CmdTest struct {
	reader io.Reader
	stdin  io.WriteCloser
	stop   chan bool
	output <-chan string
}

func NewCmdTest(cmd *exec.Cmd) *CmdTest {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	cmdOutput := &CmdTest{
		stop:   make(chan bool, 1),
		reader: pipeReader,
		stdin:  stdin,
	}
	cmdOutput.output = cmdOutput.Run()
	return cmdOutput
}

func (c *CmdTest) Run() <-chan string {
	output := make(chan string, 1024)
	go func() {
		x := 0
		for {
			x++
			b := make([]byte, 8192)
			n, err := c.reader.Read(b)
			if n > 0 {
				lines := bytes.SplitAfter(b[:n], []byte("\n"))
				// Example: Split(a\nb\n\c\n) => ["a\n", "b\n", "c\n", ""]
				// We are getting empty element because data for split was ending with delimeter (\n)
				// We don't want it, so we remove it
				lastPos := len(lines) - 1
				if len(lines[lastPos]) == 0 {
					lines = lines[:lastPos]
				}
				for i := range lines {
					log.Printf("[%d] %#v", x, string(lines[i]))
					output <- string(lines[i])
				}
			}
			if err != nil {
				break
			}
		}
	}()
	return output
}

func (c *CmdTest) Stop() {
	c.stop <- true
}

func (c *CmdTest) ReadLine() (line string) {
	select {
	case line = <-c.output:
	case <-time.After(1 * time.Second):
	}
	return line
}

func (c *CmdTest) Write(data string) {
	_, err := c.stdin.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}
