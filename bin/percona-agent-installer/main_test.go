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

package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/cmdtest"
	"github.com/percona/percona-agent/test/fakeapi"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	username        string
	basedir         string
	bin             string
	apphost         string
	serverInstance  *proto.ServerInstance
	mysqlInstance   *proto.MySQLInstance
	agent           *proto.Agent
	agentUuid       string
	fakeApi         *fakeapi.FakeApi
	apiKey          string
	rootConn        *sql.DB
	agentConfigFile string
	qanConfigFile   string
}

var _ = Suite(&MainTestSuite{})

func (s *MainTestSuite) SetUpSuite(t *C) {
	var err error

	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	s.basedir, err = ioutil.TempDir("/tmp", "agent-installer-test-")
	t.Assert(err, IsNil)
	err = os.Mkdir(s.basedir+"/"+pct.BIN_DIR, 0777)
	t.Assert(err, IsNil)
	err = os.Mkdir(s.basedir+"/"+pct.CONFIG_DIR, 0777)
	t.Assert(err, IsNil)

	s.bin = s.basedir + "/percona-agent-installer"
	cmd := exec.Command("go", "build", "-o", s.bin)
	err = cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to build installer: %s", err))

	s.username = "root"
	s.apphost = "https://cloud.percona.com"
	s.agentConfigFile = path.Join(s.basedir, pct.CONFIG_DIR, "agent.conf")
	s.qanConfigFile = path.Join(s.basedir, pct.CONFIG_DIR, "qan.conf")
	s.apiKey = "00000000000000000000000000000001"

	// Default data
	// Hostname must be correct because installer checks that
	// hostname == mysql hostname to enable QAN.
	hostname, _ := os.Hostname()
	s.serverInstance = &proto.ServerInstance{
		Id:       10,
		Hostname: hostname,
	}
	s.mysqlInstance = &proto.MySQLInstance{
		Id:       10,
		Hostname: hostname,
		DSN:      "",
	}
	s.agentUuid = "0001"
	s.agent = &proto.Agent{
		Uuid:     s.agentUuid,
		Hostname: "host1",
		Version:  "1.0.0",
		Links: map[string]string{
			"self": "http://localhost:8000/agents/" + s.agentUuid,
			"cmd":  "ws://localhost:8000/agents/" + s.agentUuid + "/cmd",
			"data": "ws://localhost:8000/agents/" + s.agentUuid + "/data",
			"log":  "ws://localhost:8000/agents/" + s.agentUuid + "/log",
		},
	}

	rootDSN := os.Getenv("PCT_TEST_MYSQL_ROOT_DSN")
	if rootDSN == "" {
		t.Fatal("PCT_TEST_MYSQL_ROOT_DSN is not set")
	}
	s.rootConn, err = sql.Open("mysql", rootDSN)
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) SetUpTest(t *C) {
	// Create fake api server
	s.fakeApi = fakeapi.NewFakeApi()

	_, err := s.rootConn.Exec("DELETE FROM mysql.user WHERE user='percona-agent'")
	t.Assert(err, IsNil)
	s.rootConn.Exec("FLUSH PRIVILEGES")
	t.Assert(err, IsNil)

	// Remove config dir between tests.
	err = os.RemoveAll(path.Join(s.basedir, pct.CONFIG_DIR))
	if err != nil {
		t.Fatal(err)
	}
}

func (s *MainTestSuite) TearDownTest(t *C) {
	// Shutdown fake api server
	s.fakeApi.Close()
}

func (s *MainTestSuite) TearDownSuite(t *C) {
	s.rootConn.Close()
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

var grantPasswordRe = regexp.MustCompile(` IDENTIFIED BY PASSWORD.+$`)

func (s *MainTestSuite) GetGrants() []string {
	grants := []string{}
	rows, err := s.rootConn.Query("SHOW GRANTS FOR 'percona-agent'@'localhost'")
	if err != nil {
		fmt.Println(err)
		return grants
	}
	for rows.Next() {
		var grant string
		err := rows.Scan(&grant)
		if err != nil {
			fmt.Println(err)
			return grants
		}
		grant = grantPasswordRe.ReplaceAllLiteralString(grant, "")
		grants = append(grants, grant)
	}
	rows.Close()

	rows, err = s.rootConn.Query("SHOW GRANTS FOR 'percona-agent'@'127.0.0.1'")
	if err != nil {
		fmt.Println(err)
		return grants
	}
	for rows.Next() {
		var grant string
		err := rows.Scan(&grant)
		if err != nil {
			fmt.Println(err)
			return grants
		}
		grant = grantPasswordRe.ReplaceAllLiteralString(grant, "")
		grants = append(grants, grant)
	}
	rows.Close()
	return grants
}

// --------------------------------------------------------------------------

func (s *MainTestSuite) TestDefaultInstall(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
	s.fakeApi.AppendConfigsMmDefaultServer()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendConfigsQanDefault()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
		"-api-key="+s.apiKey,
		//"-debug",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)
	output := cmdTest.Output()

	// Should exit zero.
	if err := cmd.Run(); err != nil {
		t.Log(output)
		log.Fatal(err)
	}

	// Should write basedir/config/agent.conf.
	if !pct.FileExists(s.agentConfigFile) {
		t.Log(output)
		t.Errorf("%s does not exist", s.agentConfigFile)
	}

	// Should write basedir/config/qan.conf.
	if !pct.FileExists(s.qanConfigFile) {
		t.Log(output)
		t.Errorf("%s does not exist", s.qanConfigFile)
	}

	// Should create percona-agent user with grants on *.* and performance_schema.*.
	got := s.GetGrants()
	expect := []string{
		"GRANT SELECT, PROCESS, SUPER ON *.* TO 'percona-agent'@'localhost'",
		"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'percona-agent'@'localhost'",
	}
	t.Check(got, DeepEquals, expect)
}

func (s *MainTestSuite) TestNonInteractiveInstallWithIgnoreFailures(t *C) {
	t.Skip("todo")

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
		"-non-interactive=true",
		"-ignore-failures=true", // We are testing this flag
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-wrong_user",
		"-api-key="+s.apiKey, // Required because of non-interactive mode
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

	t.Check(cmdTest.ReadLine(), Equals, "Specify a root/super MySQL user to create a user for the agent\n")
	t.Check(cmdTest.ReadLine(), Equals, "Auto detected DSN using `mysql --print-defaults` (use ~/.my.cnf to adjust results)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Using auto-detected DSN\n")

	t.Check(cmdTest.ReadLine(), Matches, "Testing MySQL connection wrong_user:<password-hidden>@unix(.*)...\n")
	t.Check(cmdTest.ReadLine(), Matches, "\\[MySQL\\] .* write unix .* broken pipe\n") // @todo Why "broken pipe" on wrong credentials?
	t.Check(cmdTest.ReadLine(), Matches, "Error connecting to MySQL wrong_user:<password-hidden>@unix(.*): Failed to connect to MySQL after 1 tries \\(Error 1045: Access denied for user 'wrong_user'@'localhost' \\(using password: YES\\)\\)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Failed to connect to MySQL\n")
	t.Check(cmdTest.ReadLine(), Equals, "Failed to create new MySQL account for agent\n")
	t.Check(cmdTest.ReadLine(), Equals, "Skipping creation of MySQL instance because of previous errors\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
	t.Check(cmdTest.ReadLine(), Equals, "Install successful\n")
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)
}

func (s *MainTestSuite) TestNonInteractiveInstallWithJustCredentialDetailsFlags(t *C) {
	t.Skip("todo")

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
		// "-non-interactive=true",    // This flag is automatically enabled when flags with credentials are provided
		// "-auto-detect-mysql=false", // This flag is automatically disabled when flags with credentials are provided
		"-mysql-user=root",
		"-mysql-socket=/var/run/mysqld/mysqld.sock",
		"-api-key="+s.apiKey, // Required because of non-interactive mode
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Skip("todo")

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
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
	t.Skip("todo")

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
		"-create-mysql-user=false", // We are testing this flag
		"-non-interactive=true",    // -create-mysql-user=false works only in non-interactive mode
		"-api-key="+s.apiKey,       // Required because of non-interactive mode
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-percona_user",
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Skip("todo")

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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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

func (s *MainTestSuite) TestInstallWorksWithExistingMySQLInstanceAndInstanceIsUpdated(t *C) {
	t.Skip("todo")

	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.Append("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", s.fakeApi.URL(), s.mysqlInstance.Id))
		w.WriteHeader(http.StatusConflict) // Instance already exists
	})
	s.fakeApi.Append(fmt.Sprintf("/instances/mysql/%d", s.mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&s.mysqlInstance)
			w.Write(data)
		case "PUT":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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

func (s *MainTestSuite) TestInstallFailsOnUpdatingMySQLInstance(t *C) {
	t.Skip("todo")

	// Register required api handlers
	s.fakeApi.AppendPing()
	s.fakeApi.AppendInstancesServer(s.serverInstance)
	s.fakeApi.AppendInstancesServerId(s.serverInstance)
	s.fakeApi.Append("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", s.fakeApi.URL(), s.mysqlInstance.Id))
		w.WriteHeader(http.StatusConflict) // Instance already exists
	})
	s.fakeApi.Append(fmt.Sprintf("/instances/mysql/%d", s.mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(600)
		}
	})
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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Check(cmdTest.ReadLine(), Equals, "Failed to update MySQL instance (status code 500)\n")
	t.Check(cmdTest.ReadLine(), Equals, "Install failed\n")

	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, ErrorMatches, "exit status 1")
}

func (s *MainTestSuite) TestInstallWithWrongApiKey(t *C) {
	t.Skip("todo")

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
	t.Skip("todo")

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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Skip("todo")

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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Skip("todo")

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
	cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	t.Skip("todo")

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
		"-api-key="+s.apiKey, // We are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	// Because of -api-key flag user don't provides it by hand
	//t.Check(cmdTest.ReadLine(), Equals, "API key: ")
	//cmdTest.Write(s.apiKey + "\n")
	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
	t.Check(cmdTest.ReadLine(), Equals, "API key "+s.apiKey+" is OK\n")
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
	s.fakeApi.AppendConfigsMmDefaultServer()
	//s.fakeApi.AppendConfigsMmDefaultMysql()
	//s.fakeApi.AppendSysconfigDefaultMysql()
	//s.fakeApi.AppendConfigsQanDefault()
	s.fakeApi.AppendAgents(s.agent)
	s.fakeApi.AppendAgentsUuid(s.agent)

	cmd := exec.Command(
		s.bin,
		"-basedir="+s.basedir,
		"-api-host="+s.fakeApi.URL(),
		"-api-key="+s.apiKey,
		"-mysql=false", // We are testing this flag
	)

	cmdTest := cmdtest.NewCmdTest(cmd)
	output := cmdTest.Output()

	// Should exit zero.
	if err := cmd.Run(); err != nil {
		t.Log(output)
		log.Fatal(err)
	}

	// Should write basedir/config/agent.conf.
	if !pct.FileExists(s.agentConfigFile) {
		t.Log(output)
		t.Errorf("%s does not exist", s.agentConfigFile)
	}

	// Should NOT write basedir/config/qan.conf.
	if pct.FileExists(s.qanConfigFile) {
		t.Log(output)
		t.Errorf("%s exists", s.qanConfigFile)
	}

	// Should NOT create percona-agent user with grants on *.* and performance_schema.*.
	got := s.GetGrants()
	expect := []string{}
	t.Check(got, DeepEquals, expect)
}
