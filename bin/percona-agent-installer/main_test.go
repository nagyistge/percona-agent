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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/data"
	agentLog "github.com/percona/percona-agent/log"
	mmMysql "github.com/percona/percona-agent/mm/mysql"
	mmSystem "github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	sysconfigMysql "github.com/percona/percona-agent/sysconfig/mysql"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/cmdtest"
	"github.com/percona/percona-agent/test/fakeapi"
	. "gopkg.in/check.v1"
)

const (
	ER_NONEXISTING_GRANT = 1141
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	username      string
	bin           string
	bindir        string
	apphost       string
	osInstance    *proto.Instance
	mysqlInstance *proto.Instance
	agent         *proto.Instance
	agentUUID     string
	agentLinks    map[string]string
	fakeApi       *fakeapi.FakeApi
	apiKey        string
	rootConn      *sql.DB
	configs       map[string]string
}

var _ = Suite(&MainTestSuite{})

func (s *MainTestSuite) SetUpSuite(t *C) {
	var err error

	rootDSN := os.Getenv("PCT_TEST_MYSQL_ROOT_DSN")
	if rootDSN == "" {
		t.Fatal("PCT_TEST_MYSQL_ROOT_DSN is not set")
	}
	s.rootConn, err = sql.Open("mysql", rootDSN)
	t.Assert(err, IsNil)

	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	basedir, err := ioutil.TempDir("/tmp", "agent-installer-test-basedir-")
	t.Assert(err, IsNil)
	pct.Basedir.Init(basedir)

	s.bindir, err = ioutil.TempDir("/tmp", "agent-installer-test-bin-")
	t.Assert(err, IsNil)
	s.bin = s.bindir + "/percona-agent-installer"
	cmd := exec.Command("go", "build", "-o", s.bin)
	err = cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to build installer: %s", err))

	s.username = "root"
	s.apphost = "https://cloud.percona.com"
	s.apiKey = "00000000000000000000000000000001"

	// Default data
	// Hostname must be correct because installer checks that
	// hostname == mysql hostname to enable QAN.
	hostname, _ := os.Hostname()
	s.osInstance = &proto.Instance{}
	s.osInstance.Type = "OS"
	s.osInstance.Prefix = "os"
	s.osInstance.UUID = "1"
	s.osInstance.Name = hostname
	s.osInstance.Created = time.Time{}
	s.osInstance.Deleted = time.Time{}

	s.agent = &proto.Instance{}
	s.agent.Type = "Percona Agent"
	s.agent.Prefix = "agent"
	s.agent.UUID = "2"
	s.agent.Name = hostname
	s.agent.ParentUUID = s.osInstance.UUID

	s.mysqlInstance = &proto.Instance{}
	s.mysqlInstance.UUID = "3"
	s.mysqlInstance.Name = hostname
	s.mysqlInstance.DSN = ""
	s.mysqlInstance.ParentUUID = s.osInstance.UUID

	s.osInstance.Subsystems = []proto.Instance{*s.agent, *s.mysqlInstance}

	s.agent.Properties = map[string]string{"version": agent.VERSION}
}

func (s *MainTestSuite) SetUpTest(t *C) {
	// Create fake api server
	s.fakeApi = fakeapi.NewFakeApi()
	// Expected agent links
	s.agentLinks = map[string]string{
		"log":         s.fakeApi.WSURL() + "/instances/" + s.agent.UUID + "/log",
		"self":        s.fakeApi.URL() + "/instances/" + s.agent.UUID,
		"system_tree": s.fakeApi.URL() + "/instances/" + s.osInstance.UUID + "?recursive=true",
		"cmd":         s.fakeApi.WSURL() + "/instances/" + s.agent.UUID + "/cmd",
		"data":        s.fakeApi.WSURL() + "/instances/" + s.agent.UUID + "/data",
	}

	_, err := s.rootConn.Exec("DELETE FROM mysql.user WHERE user='percona-agent'")
	t.Check(err, IsNil)
	s.rootConn.Exec("FLUSH PRIVILEGES")
	t.Check(err, IsNil)

	// Remove config dir between tests.
	err = os.RemoveAll(pct.Basedir.Path())
	t.Check(err, IsNil)
}

func (s *MainTestSuite) TearDownTest(t *C) {
	// Shutdown fake api server
	s.fakeApi.Close()
}

func (s *MainTestSuite) TearDownSuite(t *C) {
	s.rootConn.Close()
	err := os.RemoveAll(pct.Basedir.Path())
	t.Check(err, IsNil)
	err = os.RemoveAll(s.bindir)
	t.Check(err, IsNil)
}

var grantPasswordRe = regexp.MustCompile(` IDENTIFIED BY PASSWORD.+$`)

func (s *MainTestSuite) GetGrants() []string {
	grants := []string{}
	rowsLocalhost, err := s.rootConn.Query("SHOW GRANTS FOR 'percona-agent'@'localhost'")
	if val, ok := err.(*mysql.MySQLError); ok && val.Number == ER_NONEXISTING_GRANT {
		// Error: 1141 SQLSTATE: 42000 (ER_NONEXISTING_GRANT)
		return grants
	} else if err != nil {
		panic(err)
	}
	defer rowsLocalhost.Close()

	for rowsLocalhost.Next() {
		var grant string
		err := rowsLocalhost.Scan(&grant)
		if err != nil {
			fmt.Println(err)
			return grants
		}
		grant = grantPasswordRe.ReplaceAllLiteralString(grant, "")
		grants = append(grants, grant)
	}

	rows127001, err := s.rootConn.Query("SHOW GRANTS FOR 'percona-agent'@'127.0.0.1'")
	if val, ok := err.(*mysql.MySQLError); ok && val.Number == ER_NONEXISTING_GRANT {
		// Error: 1141 SQLSTATE: 42000 (ER_NONEXISTING_GRANT)
		return grants
	} else if err != nil {
		panic(err)
	}

	defer rows127001.Close()

	for rows127001.Next() {
		var grant string
		err := rows127001.Scan(&grant)
		if err != nil {
			fmt.Println(err)
			return grants
		}
		grant = grantPasswordRe.ReplaceAllLiteralString(grant, "")
		grants = append(grants, grant)
	}

	return grants
}

// --------------------------------------------------------------------------

func (s *MainTestSuite) TestDefaultInstall(t *C) {
	// Register required api handlers
	s.fakeApi.AppendPing()
	//osInstance := &proto.Instance{UUID: s.osInstance.UUID}
	queueInstances := []*proto.Instance{s.osInstance, s.agent, s.mysqlInstance}
	s.fakeApi.AppendInstances(nil, queueInstances) // GET instance first, POST instances UUIDs second
	s.fakeApi.AppendInstancesUUID(s.osInstance)
	s.fakeApi.AppendInstancesUUID(s.agent)
	s.fakeApi.AppendInstancesUUID(s.mysqlInstance)
	s.fakeApi.AppendSystemTree(s.osInstance)
	s.fakeApi.AppendConfigsMmDefaultOS()
	s.fakeApi.AppendConfigsMmDefaultMysql()
	s.fakeApi.AppendSysconfigDefaultMysql()
	s.fakeApi.AppendConfigsQanDefault()

	cmd := exec.Command(
		s.bin,
		"-basedir="+pct.Basedir.Path(),
		"-api-host="+s.fakeApi.URL(),
		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
		"-api-key="+s.apiKey,
	)

	cmdTest := cmdtest.NewCmdTest(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created OS: name=%s uuid=%d\n", s.osInstance.Name, s.osInstance.UUID))
	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.UUID))

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s name=%s uuid=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Name, s.mysqlInstance.UUID))
	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

	err := cmd.Wait()
	t.Assert(err, IsNil)

	s.expectConfigs(
		[]string{
			"agent.conf",
			"data.conf",
			"log.conf",
			fmt.Sprintf("mm-%s.conf", s.osInstance.UUID),
			fmt.Sprintf("mm-%s.conf", s.mysqlInstance.UUID),
			"qan.conf",
			fmt.Sprintf("sysconfig-%s.conf", s.mysqlInstance.UUID),
			"system-tree.json",
		},
		t,
	)

	s.expectDefaultAgentConfig(t)
	s.expectDefaultDataConfig(t)
	s.expectDefaultLogConfig(t)
	s.expectDefaultMmMysqlConfig(t)
	s.expectDefaultMmOSConfig(t)
	s.expectDefaultQanConfig(t)
	s.expectDefaultSysconfigMysqlConfig(t)
	// Should create percona-agent user with grants on *.* and performance_schema.*.
	s.expectMysqlUserExists(t)
}

//func (s *MainTestSuite) TestNonInteractiveInstallWithJustCredentialDetailsFlags(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-interactive=false",
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-wrong_user",
//		"-mysql-user="+s.username,
//		"-mysql-socket=/var/run/mysqld/mysqld.sock",
//		"-api-key="+s.apiKey, // Required because of non-interactive mode
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// Should create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserExists(t)
//}
//func (s *MainTestSuite) TestNonInteractiveInstallWithMissingApiKey(t *C) {
//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-interactive=false",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "API key is required, please provide it with -api-key option.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API Key is available at "+s.apphost+"/api-key\n")

//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, ErrorMatches, "exit status 1")

//	s.expectConfigs([]string{}, t)
//	s.expectMysqlUserNotExists(t)
//}

//func (s *MainTestSuite) TestNonInteractiveInstallWithFlagCreateMySQLUserFalse(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-create-mysql-user=false", // We are testing this flag
//		"-interactive=false",       // -create-mysql-user=false works only in non-interactive mode
//		"-api-key="+s.apiKey,       // Required because of non-interactive mode
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-percona_user",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, "Skip creating MySQL user (-create-mysql-user=false)\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// -create-mysql-user=false
//	s.expectMysqlUserNotExists(t)
//}
//func (s *MainTestSuite) TestWithAgentMySQLUser(t *C) {

//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	s.fakeApi.AppendInstancesServer(1, s.serverInstance)
//	s.fakeApi.AppendInstancesServerId(1, s.serverInstance)
//	s.fakeApi.AppendInstancesMysql(1, s.mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(1, s.mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	user := "some-user"
//	pass := "some-pass"
//	host := "localhost"
//	maxCons := 10

//	// Create a temporary user because on some installations the default user & pass
//	// are empty and for testing -agent-mysql-user & -agent-mysql-pass, we need non-empty
//	// user & pass parameters
//	grantQuery := fmt.Sprintf("GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d", user, host, pass, maxCons)
//	s.rootConn.Exec(grantQuery)

//	basedir, err := ioutil.TempDir("/tmp", "agent-installer-test-basedir-")
//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+basedir,
//		"-api-host="+s.fakeApi.URL(),
//		"-agent-mysql-user="+user,
//		"-agent-mysql-pass="+pass,
//		"-interactive=false",
//		"-api-key="+s.apiKey,
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Created agent: uuid=0001\n")
//	// Use the s flag (?s) to let .* match \n
//	t.Assert(cmdTest.ReadLine(), Matches, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "Using provided user/pass for mysql-agent user. DSN: some-user:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n")
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", s.mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "")

//	err = cmd.Wait()
//	t.Assert(err, IsNil)

//	// Remove the user we just created for this test
//	s.rootConn.Exec(fmt.Sprintf("DROP USER '%s'@'%s'", user, host))
//}

//func (s *MainTestSuite) TestInstall(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.Append("/agents", func(w http.ResponseWriter, r *http.Request) {
//		// Validate
//		expectedProtoAgent := proto.Agent{
//			Uuid:     "",
//			Hostname: s.agent.Hostname,
//			Version:  s.agent.Version,
//		}
//		protoAgent := proto.Agent{}
//		body, err := ioutil.ReadAll(r.Body)
//		t.Assert(err, IsNil)
//		err = json.Unmarshal(body, &protoAgent)
//		t.Assert(err, IsNil)
//		for i := range protoAgent.Configs {
//			protoAgent.Configs[i].Updated = time.Time{}
//		}
//		t.Assert(protoAgent, DeepEquals, expectedProtoAgent)

//		// Send response
//		w.Header().Set("Location", fmt.Sprintf("%s/agents/%s", s.fakeApi.URL(), s.agent.Uuid))
//		w.WriteHeader(http.StatusCreated)
//	})
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// Should create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserExists(t)
//}

//func (s *MainTestSuite) TestInstallFailsOnAgentsLimit(t *C) {
//	agentLimit := 5

//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	s.fakeApi.Append("/agents", func(w http.ResponseWriter, r *http.Request) {
//		w.Header().Set("X-Percona-Agents-Limit", fmt.Sprintf("%d", agentLimit))
//		// Send response
//		w.WriteHeader(http.StatusForbidden)
//	})

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Maximum number of %d agents exceeded.\n", agentLimit))
//	t.Check(cmdTest.ReadLine(), Equals, "Go to https://cloud.percona.com/agents and remove unused agents or contact Percona to increase limit.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Check(err, ErrorMatches, "exit status 1")

//	s.expectConfigs([]string{}, t)
//	s.expectMysqlUserNotExists(t)
//}
//func (s *MainTestSuite) TestInstallWorksWithExistingMySQLInstanceAndInstanceIsUpdated(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.Append("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
//		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", s.fakeApi.URL(), s.mysqlInstance.Id))
//		w.WriteHeader(http.StatusConflict) // Instance already exists
//	})
//	s.fakeApi.Append(fmt.Sprintf("/instances/mysql/%d", s.mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
//		switch r.Method {
//		case "GET":
//			w.WriteHeader(http.StatusOK)
//			data, _ := json.Marshal(mysqlInstance)
//			w.Write(data)
//		case "PUT":
//			body, err := ioutil.ReadAll(r.Body)
//			if err != nil {
//				panic(err)
//			}
//			err = json.Unmarshal(body, mysqlInstance)
//			if err != nil {
//				panic(err)
//			}
//			mysqlInstance.Id = s.mysqlInstance.Id
//			w.WriteHeader(http.StatusOK)
//		default:
//			w.WriteHeader(600)
//		}
//	})
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// Should create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserExists(t)
//}

//func (s *MainTestSuite) TestInstallFailsOnUpdatingMySQLInstance(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	s.fakeApi.Append("/instances/mysql", func(w http.ResponseWriter, r *http.Request) {
//		w.Header().Set("Location", fmt.Sprintf("%s/instances/mysql/%d", s.fakeApi.URL(), s.mysqlInstance.Id))
//		w.WriteHeader(http.StatusConflict) // Instance already exists
//	})
//	s.fakeApi.Append(fmt.Sprintf("/instances/mysql/%d", s.mysqlInstance.Id), func(w http.ResponseWriter, r *http.Request) {
//		switch r.Method {
//		case "PUT":
//			w.WriteHeader(http.StatusInternalServerError)
//		default:
//			w.WriteHeader(600)
//		}
//	})
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Failed to update MySQL instance (status code 500)\n")
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, ErrorMatches, "exit status 1")

//	s.expectConfigs([]string{}, t)
//	s.expectMysqlUserExists(t) // @todo this is wrong
//}

//func (s *MainTestSuite) TestInstallWithWrongApiKey(t *C) {
//	s.fakeApi.Append("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		w.WriteHeader(http.StatusInternalServerError)
//	}))

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	apiKey := "WrongApiKey"
//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Assert(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(apiKey + "\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+apiKey+"...\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Sorry, there's an API problem (status code 500). Please try to install again. If the problem continues, contact Percona.\n")

//	t.Assert(cmdTest.ReadLine(), Equals, "Try again? (Y): ")
//	cmdTest.Write("N\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Failed to verify API key\n")

//	t.Assert(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, ErrorMatches, "exit status 1")

//	s.expectConfigs([]string{}, t)
//	s.expectMysqlUserNotExists(t)
//}

//// todo what's the point of -create-agent flag? for what is it usefull?
//func (s *MainTestSuite) TestInstallWithFlagCreateAgentFalse(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	// Flag -create-agent=false implies that agent
//	// shouldn't query below api routes
//	//s.fakeApi.AppendAgents(s.agent)
//	//s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//		"-create-agent=false", // we are testing this flag
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Not creating agent (-create-agent=false)\n"))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			//fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			//fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			//"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			//fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	//s.expectDefaultMmMysqlConfig(t)
//	//s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	//s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	//s.expectDefaultSysconfigMysqlConfig(t)

//	s.expectMysqlUserExists(t)
//}

//func (s *MainTestSuite) TestInstallWithFlagOldPasswordsTrue(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	mysqlInstance := &proto.MySQLInstance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.Id, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//		"-old-passwords=true",
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)

//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "No API Key Defined.\n")
//	t.Check(cmdTest.ReadLine(), Equals, "Please Enter your API Key, it is available at "+s.apphost+"/api-key\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API key: ")
//	cmdTest.Write(s.apiKey + "\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	// Flag -old-passwords=true should add &allowOldPasswords=true to DSN
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true&allowOldPasswords=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// Should create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserExists(t)
//}

//func (s *MainTestSuite) TestInstallWithFlagApiKey(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.Instance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.UUID, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.UUID, serverInstance)
//	mysqlInstance := &proto.Instance{}
//	s.fakeApi.AppendInstancesMysql(s.mysqlInstance.UUID, mysqlInstance)
//	s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance.UUID, mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	s.fakeApi.AppendConfigsMmDefaultMysql()
//	s.fakeApi.AppendSysconfigDefaultMysql()
//	s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-mysql-defaults-file="+test.RootDir+"/installer/my.cnf-root_user",
//		"-api-key="+s.apiKey, // We are testing this flag
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)
//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("MySQL root DSN: %s:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)\n", s.username))
//	t.Check(cmdTest.ReadLine(), Equals, "Created MySQL user: percona-agent:<password-hidden>@unix(/var/run/mysqld/mysqld.sock)/?parseTime=true\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mysqlInstance.DSN, s.mysqlInstance.Hostname, s.mysqlInstance.Id))
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-mysql-%d.conf", s.mysqlInstance.Id),
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("mysql-%d.conf", s.mysqlInstance.Id),
//			"qan.conf",
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("sysconfig-mysql-%d.conf", s.mysqlInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmMysqlConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectMysqlConfig(*mysqlInstance, t)
//	s.expectDefaultQanConfig(t)
//	s.expectServerConfig(*serverInstance, t)
//	s.expectDefaultSysconfigMysqlConfig(t)

//	// Should create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserExists(t)
//}

//func (s *MainTestSuite) TestInstallWithFlagMysqlFalse(t *C) {
//	// Register required api handlers
//	s.fakeApi.AppendPing()
//	serverInstance := &proto.ServerInstance{}
//	s.fakeApi.AppendInstancesServer(s.serverInstance.Id, serverInstance)
//	s.fakeApi.AppendInstancesServerId(s.serverInstance.Id, serverInstance)
//	// Flag -mysql=false implies that agent
//	// shouldn't query below api routes
//	//s.fakeApi.AppendInstancesMysql(s.mysqlInstance)
//	//s.fakeApi.AppendInstancesMysqlId(s.mysqlInstance)
//	s.fakeApi.AppendConfigsMmDefaultServer()
//	//s.fakeApi.AppendConfigsMmDefaultMysql()
//	//s.fakeApi.AppendSysconfigDefaultMysql()
//	//s.fakeApi.AppendConfigsQanDefault()
//	s.fakeApi.AppendAgents(s.agent)
//	s.fakeApi.AppendAgentsUuid(s.agent)

//	cmd := exec.Command(
//		s.bin,
//		"-basedir="+pct.Basedir.Path(),
//		"-api-host="+s.fakeApi.URL(),
//		"-api-key="+s.apiKey,
//		"-mysql=false", // We are testing this flag
//	)

//	cmdTest := cmdtest.NewCmdTest(cmd)
//	if err := cmd.Start(); err != nil {
//		log.Fatal(err)
//	}

//	t.Check(cmdTest.ReadLine(), Equals, "CTRL-C at any time to quit\n")
//	t.Check(cmdTest.ReadLine(), Equals, "API host: "+s.fakeApi.URL()+"\n")

//	t.Check(cmdTest.ReadLine(), Equals, "Verifying API key "+s.apiKey+"...\n")

//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created agent: uuid=%s\n", s.agent.Uuid))
//	t.Check(cmdTest.ReadLine(), Equals, fmt.Sprintf("Created server instance: hostname=%s id=%d\n", s.serverInstance.Hostname, s.serverInstance.Id))

//	t.Check(cmdTest.ReadLine(), Equals, "Not starting MySQL services (-start-mysql-services=false)\n")
//	t.Check(cmdTest.ReadLine(), Equals, "") // No more data

//	err := cmd.Wait()
//	t.Assert(err, IsNil)

//	s.expectConfigs(
//		[]string{
//			"agent.conf",
//			"data.conf",
//			"log.conf",
//			fmt.Sprintf("mm-server-%d.conf", s.serverInstance.Id),
//			fmt.Sprintf("server-%d.conf", s.serverInstance.Id),
//		},
//		t,
//	)

//	s.expectDefaultAgentConfig(t)
//	s.expectDefaultDataConfig(t)
//	s.expectDefaultLogConfig(t)
//	s.expectDefaultMmServerConfig(t)
//	s.expectServerConfig(*serverInstance, t)

//	// Should NOT create percona-agent user with grants on *.* and performance_schema.*.
//	s.expectMysqlUserNotExists(t)
//}

func (s *MainTestSuite) expectConfigs(expectedConfigs []string, t *C) {
	gotConfigs := []string{}
	fileinfos, err := ioutil.ReadDir(pct.Basedir.Dir("config"))
	t.Check(err, IsNil)
	for _, fileinfo := range fileinfos {
		gotConfigs = append(gotConfigs, fileinfo.Name())
	}
	t.Check(gotConfigs, DeepEquals, expectedConfigs)
}

func (s *MainTestSuite) expectDefaultAgentConfig(t *C) {
	expectedConfig := agent.Config{
		AgentUUID:   s.agent.UUID,
		ApiHostname: s.fakeApi.URL(),
		ApiKey:      s.apiKey,
		Keepalive:   0,
		Links:       s.agentLinks,
		PidFile:     "percona-agent.pid",
	}

	gotConfig := agent.Config{}
	if err := pct.Basedir.ReadConfig("agent", &gotConfig); err != nil {
		t.Errorf("Read agent config: %s", err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectDefaultDataConfig(t *C) {
	expectedConfig := data.Config{
		Encoding:     data.DEFAULT_DATA_ENCODING,
		SendInterval: data.DEFAULT_DATA_SEND_INTERVAL,
		Blackhole:    false,
	}

	gotConfig := data.Config{}
	if err := pct.Basedir.ReadConfig("data", &gotConfig); err != nil {
		t.Errorf("Read agent config: %s", err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectDefaultLogConfig(t *C) {
	expectedConfig := agentLog.Config{
		Level:   agentLog.DEFAULT_LOG_LEVEL,
		File:    agentLog.DEFAULT_LOG_FILE,
		Offline: false,
	}

	gotConfig := agentLog.Config{}
	if err := pct.Basedir.ReadConfig("log", &gotConfig); err != nil {
		t.Errorf("Read agent config: %s", err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectDefaultMmMysqlConfig(t *C) {
	tool := "mm"
	instanceUUID := s.mysqlInstance.UUID
	expectedConfig := fakeapi.ConfigMmDefaultMysql
	expectedConfig.UUID = instanceUUID

	gotConfig := mmMysql.Config{}
	if err := pct.Basedir.ReadInstanceConfig(tool, instanceUUID, &gotConfig); err != nil {
		t.Errorf("Read %s-%s config: %v", tool, instanceUUID, err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectDefaultMmOSConfig(t *C) {
	instanceUUID := s.osInstance.UUID
	expectedConfig := fakeapi.ConfigMmDefaultOS
	expectedConfig.UUID = instanceUUID

	gotConfig := mmSystem.Config{}
	if err := pct.Basedir.ReadInstanceConfig("mm", instanceUUID, &gotConfig); err != nil {
		t.Errorf("Read mm-%s config: %v", instanceUUID, err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

// TODO delete
//func (s *MainTestSuite) expectMysqlConfig(expectedConfig proto.MySQLInstance, t *C) {
//	service := "mysql"
//	instanceUUID := expectedConfig.UUID
//	gotConfig := proto.Instance{}
//	// TODO use ReadInstanceConfig?
//	if err := pct.Basedir.ReadConfig(fmt.Sprintf("%s-%d", service, instanceUUID), &gotConfig); err != nil {
//		t.Errorf("Read %s-%d config: %s", service, instanceUUID, err)
//	}

//	t.Check(gotConfig, DeepEquals, expectedConfig)
//}

func (s *MainTestSuite) expectDefaultQanConfig(t *C) {
	expectedConfig := qan.Config{
		UUID:              s.mysqlInstance.UUID,
		CollectFrom:       "",
		MaxWorkers:        0,
		Interval:          60,
		MaxSlowLogSize:    0,
		RemoveOldSlowLogs: false,
		ExampleQueries:    false,
		WorkerRunTime:     0,
		ReportLimit:       0,
	}

	gotConfig := qan.Config{}
	if err := pct.Basedir.ReadConfig("qan", &gotConfig); err != nil {
		t.Errorf("Read qan config: %s", err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectDefaultSysconfigMysqlConfig(t *C) {
	instanceUUID := s.mysqlInstance.UUID
	expectedConfig := fakeapi.ConfigSysconfigDefaultMysql
	expectedConfig.UUID = instanceUUID
	gotConfig := sysconfigMysql.Config{}
	tool := "sysconfig"
	if err := pct.Basedir.ReadInstanceConfig(tool, instanceUUID, &gotConfig); err != nil {
		t.Errorf("Read sysconfig-%s config: %s", tool, instanceUUID, err)
	}

	t.Check(gotConfig, DeepEquals, expectedConfig)
}

func (s *MainTestSuite) expectMysqlUserExists(t *C) {
	got := s.GetGrants()
	expect := []string{
		"GRANT SELECT, PROCESS, SUPER ON *.* TO 'percona-agent'@'localhost'",
		"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'percona-agent'@'localhost'",
	}
	t.Check(got, DeepEquals, expect)
}

func (s *MainTestSuite) expectMysqlUserNotExists(t *C) {
	got := s.GetGrants()
	expect := []string{}
	t.Check(got, DeepEquals, expect)
}
