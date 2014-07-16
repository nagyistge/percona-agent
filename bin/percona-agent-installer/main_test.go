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
	"bufio"
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

	t.Assert(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Assert(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	apiKey := "WrongApiKey"
	t.Assert(cmdTest.ReadLine(), Equals, "API key: "+apiKey+"\n")
	cmdTest.Write(apiKey + "\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Sorry, there's an API problem (status code 500). Please try to install again. If the problem continues, contact Percona.\n")

	cmdTest.Write("N\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Try again? (Y): N\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Failed to verify API key\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Install failed\n")

	t.Assert(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, ErrorMatches, "exit status 1")
}

func (s *MainTestSuite) TestBasicInstall(t *C) {
	serverInstance := proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	url := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
		} else if r.URL.Path == "/instances/server" {
			w.Header().Set("Location", url+"/instances/server/10")
			w.WriteHeader(http.StatusCreated)
		} else if r.URL.Path == "/instances/server/10" {
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&serverInstance)
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

	t.Assert(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Assert(cmdTest.ReadLine(), Equals, "API host: "+ts.URL+"\n")

	// @todo: The main problem is with questions that require user input,
	// @todo: they don't end with \n so we can't split stdin by lines
	// @todo: My first version fetched data from stdin in goroutine
	// @todo: and passed it to channel... but again, we need to split data by \n
	// @todo: and pass line by line to channel...
	// @todo: I don't see any good solution for this so I will probably came with something hacky.
	t.Assert(cmdTest.ReadQuestion(), Equals, "API key:")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Assert(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
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

	t.Check(cmdTest.ReadLine(), Equals, "")

	t.Assert(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, ErrorMatches, "exit status 1")
}

type CmdTest struct {
	reader *bufio.Reader
	stdin  io.WriteCloser
	stop   chan bool
}

func NewCmdTest(cmd *exec.Cmd) *CmdTest {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter
	reader := bufio.NewReader(pipeReader)

	cmdOutput := &CmdTest{
		stop:   make(chan bool, 1),
		reader: reader,
		stdin:  stdin,
	}
	return cmdOutput
}

func (c *CmdTest) Stop() {
	c.stop <- true
}

func (c *CmdTest) ReadLine() (line string) {
	line, _ = c.reader.ReadString('\n')
	return line
}

func (c *CmdTest) ReadQuestion() (line string) {
	line, _ = c.reader.ReadString(':')
	return line
}

func (c *CmdTest) Write(data string) {
	_, err := c.stdin.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}
