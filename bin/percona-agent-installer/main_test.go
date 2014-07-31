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
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/cmdtest"
	"github.com/percona/percona-agent/test/fakeapi"
	. "launchpad.net/gocheck"
	"log"
	"net/http"
	"os"
	"os/exec"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	username       string
	basedir        string
	bin            string
	apphost        string
	serverInstance *proto.ServerInstance
	mysqlInstance  *proto.MySQLInstance
	agent          *proto.Agent
	agentUuid      string
	fakeApi        *fakeapi.FakeApi
}

var _ = Suite(&MainTestSuite{
	username: "root",
	basedir:  "/tmp/percona-agent-installer-test",
	bin:      "/tmp/test-agent-installer",
	apphost:  "https://cloud.percona.com",
})

func (s *MainTestSuite) SetUpSuite(t *C) {
	cmd := exec.Command("go", "build", "-o", s.bin, "github.com/percona/percona-agent/bin/percona-agent-installer")
	err := cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to build installer: %s", err))

	// Default data
	s.serverInstance = &proto.ServerInstance{
		Id:       10,
		Hostname: "localhost",
	}
	s.mysqlInstance = &proto.MySQLInstance{
		Id:       10,
		Hostname: "localhost",
		DSN:      "",
	}
	s.agentUuid = "0001"
	s.agent = &proto.Agent{
		Uuid:     s.agentUuid,
		Hostname: "host1",
		Alias:    "master-db",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + s.agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + s.agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + s.agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + s.agentUuid + "/log",
		},
	}
}

func (s *MainTestSuite) TearDownSuite(c *C) {

	// Remove test installer binary
	err := os.Remove(s.bin)
	c.Check(err, IsNil)
}

func (s *MainTestSuite) SetUpTest(c *C) {
	// Create fake api server
	s.fakeApi = fakeapi.NewFakeApi()
}

func (s *MainTestSuite) TearDownTest(c *C) {
	// Shutdown fake api server
	s.fakeApi.Close()
}

// --------------------------------------------------------------------------
func (s *MainTestSuite) TestNonInteractiveInstall(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	apiKey := "00000000000000000000000000000001"
	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-non-interactive=true", // We are testing this flag
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
		"-api-key="+apiKey, // Required because of non-interactive mode
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")
	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Using auto-detected DSN\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)/?parseTime=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestNonInteractiveInstallWithJustCredentialDetailsFlags(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	apiKey := "00000000000000000000000000000001"
	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		// "-non-interactive=true",    // This flag is automatically enabled when flags with credentials are provided
		// "-auto-detect-mysql=false", // This flag is automatically disabled when flags with credentials are provided
		"-mysql-user=root",
		"-mysql-socket=/var/run/mysqld/mysqld.sock",
		"-api-key="+apiKey, // Required because of non-interactive mode
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}
func (s *MainTestSuite) TestNonInteractiveInstallWithMissingApiKey(t *C) {
	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-non-interactive=true",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "API key is required, please provide it with -api-key option.\n")
	t.Check(cmdTest.ReadLine(), Equals, "API Key is available at "+s.apphost+"/api-key\n")

	t.Check(cmdTest.ReadLine(), Equals, "Install failed\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, ErrorMatches, "exit status 1")
}

func (s *MainTestSuite) TestNonInteractiveInstallWithFlagCreateMySQLUserFalse(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	apiKey := "00000000000000000000000000000001"
	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-create-mysql-user=false", // We are testing this flag
		"-non-interactive=true",    // -create-mysql-user=false works only in non-interactive mode
		"-api-key="+apiKey,         // Required because of non-interactive mode
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-percona_user",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Skip creating MySQL user (-create-mysql-user=false)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify the existing MySQL user to use for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Using auto-detected DSN\n")

	t.Check(cmdTest.ReadLine(), Equals, "Using existing MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona:<password-hidden>@unix(.*)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstall(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Assert(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")

	/**
	 * MySQL super user credentials
	 */
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Matches, "Auto detected MySQL connection details: .*:<password-hidden>@unix(.+)\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Use auto-detected connection details? (Y): ")
	cmdTest.Write("N\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	/**
	 * MySQL new user
	 */
	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)\n")

	/**
	 * MySQL instance
	 */
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstallWithWrongApiKey(t *C) {
	s.fakeApi.Append("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	apiKey := "WrongApiKey"
	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
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

func (s *MainTestSuite) TestInstallWithExistingMySQLUser(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Assert(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("N\n")

	/**
	 * Using existing MySQL user
	 */
	t.Check(cmdTest.ReadLine(), Equals, "Specify the existing MySQL user to use for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Matches, "Auto detected MySQL connection details: .*:<password-hidden>@unix(.+)\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Use auto-detected connection details? (Y): ")
	cmdTest.Write("N\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Equals, "Using existing MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: "+s.username+":<password-hidden>@unix(.*)/?parseTime=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstallWithFlagCreateAgentFalse(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	// Flag -create-agent=false implies that agent
	// shouldn't query below api routes
	//s.fakeApi.AppendAgents(s.agent)
	//s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-create-agent=false", // we are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Assert(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Matches, "Auto detected MySQL connection details: .*:<password-hidden>@unix(.+)\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Use auto-detected connection details? (Y): ")
	cmdTest.Write("N\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Not creating agent (-create-agent=false)\n"))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstallWithFlagOldPasswordsTrue(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-old-passwords=true",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Assert(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Matches, "Auto detected MySQL connection details: .*:<password-hidden>@unix(.+)\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Use auto-detected connection details? (Y): ")
	cmdTest.Write("N\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	// Flag -old-passwords=true should add &allowOldPasswords=true to DSN
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)/?parseTime=true&allowOldPasswords=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstallWithFlagApiKey(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	apiKey := "00000000000000000000000000000001"
	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-api-key="+apiKey, // We are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	// Because of -api-key flag user don't provides it by hand
	//t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	//cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Assert(cmdTest.ReadLine(), Equals, "Create MySQL user for agent? ('N' to use existing user) (Y): ")
	cmdTest.Write("Y\n")
	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")

	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Matches, "Auto detected MySQL connection details: .*:<password-hidden>@unix(.+)\n")
	t.Assert(cmdTest.ReadLine(), Equals, "Use auto-detected connection details? (Y): ")
	cmdTest.Write("N\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL username: ")
	mysqlUserName := "root"
	cmdTest.Write(mysqlUserName + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL password: ")
	mysqlPassword := ""
	cmdTest.Write(mysqlPassword + "\n")

	t.Assert(cmdTest.ReadLine(), Equals, "MySQL host[:port] or socket file (localhost): ")
	mysqlHost := ""
	cmdTest.Write(mysqlHost + "\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection "+s.username+":<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")

	t.Check(cmdTest.ReadLine(), Equals, "Creating new MySQL user for agent...\n")
	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection percona-agent:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Equals, "MySQL connection OK\n")
	t.Check(cmdTest.ReadLine(), Matches, "Agent MySQL user: percona-agent:<password-hidden>@unix(.*)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestInstallWithFlagMysqlFalse(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	// Flag -mysql=false implies that agent
	// shouldn't query below api routes
	//s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	//s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	//s.fakeApi.AppendConfigsMmDefaultMysql()
	//s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-plain-passwords=true",
		"-mysql=false", // We are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	apiKey := "00000000000000000000000000000001"
	cmdTest.Write(apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	// Flag -mysql=false implies below
	t.Check(cmdTest.ReadLine(), Equals, "Not creating MySQL instance (-create-mysql-instance=false)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Not starting MySQL services (-start-mysql-services=false)\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}
