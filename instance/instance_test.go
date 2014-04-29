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

package instance_test

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"path/filepath"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type RepoTestSuite struct {
	tmpDir    string
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	configDir string
	api       *mock.API
}

var _ = Suite(&RepoTestSuite{})

func (s *RepoTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "pct-it-test")

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
}

func (s *RepoTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.configDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *RepoTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *RepoTestSuite) TestInit(t *C) {
	im := instance.NewRepo(s.logger, s.configDir, s.api)
	t.Assert(im, NotNil)

	err := im.Init()
	t.Check(err, IsNil)

	err = test.CopyFile(test.RootDir+"/mm/config/mysql-1.conf", s.configDir)
	t.Assert(err, IsNil)

	err = im.Init()
	t.Assert(err, IsNil)

	mysqlIt := &proto.MySQLInstance{}
	err = im.Get("mysql", 1, mysqlIt)
	t.Assert(err, IsNil)
	expect := &proto.MySQLInstance{
		Id:       1,
		Hostname: "db1",
		DSN:      "user:host@tcp:(127.0.0.1:3306)",
		Distro:   "Percona Server",
		Version:  "5.6.16",
	}

	if same, diff := test.IsDeeply(mysqlIt, expect); !same {
		test.Dump(mysqlIt)
		test.Dump(expect)
		t.Error(diff)
	}
}

func (s *RepoTestSuite) TestAddRemove(t *C) {
	im := instance.NewRepo(s.logger, s.configDir, s.api)
	t.Assert(im, NotNil)

	t.Check(test.FileExists(s.configDir+"/mysql-1.conf"), Equals, false)

	mysqlIt := &proto.MySQLInstance{
		Id:       1,
		Hostname: "db1",
		DSN:      "user:host@tcp:(127.0.0.1:3306)",
		Distro:   "Percona Server",
		Version:  "5.6.16",
	}
	data, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)
	err = im.Add("mysql", 1, data, true)
	t.Assert(err, IsNil)

	t.Check(test.FileExists(s.configDir+"/mysql-1.conf"), Equals, true)

	got := &proto.MySQLInstance{}
	err = im.Get("mysql", 1, got)
	t.Assert(err, IsNil)
	if same, diff := test.IsDeeply(got, mysqlIt); !same {
		t.Error(diff)
	}

	data, err = ioutil.ReadFile(s.configDir + "/mysql-1.conf")
	t.Assert(err, IsNil)

	got = &proto.MySQLInstance{}
	err = json.Unmarshal(data, got)
	t.Assert(err, IsNil)
	if same, diff := test.IsDeeply(got, mysqlIt); !same {
		t.Error(diff)
	}

	im.Remove("mysql", 1)
	t.Check(test.FileExists(s.configDir+"/mysql-1.conf"), Equals, false)
}

func (s *RepoTestSuite) TestErrors(t *C) {
	im := instance.NewRepo(s.logger, s.configDir, s.api)
	t.Assert(im, NotNil)

	mysqlIt := &proto.MySQLInstance{
		Id:       0,
		Hostname: "db1",
		DSN:      "user:host@tcp:(127.0.0.1:3306)",
		Distro:   "Percona Server",
		Version:  "5.6.16",
	}
	data, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)

	// Instance ID must be > 0.
	err = im.Add("mysql", 0, data, false)
	t.Assert(err, NotNil)

	// Service name must be one of proto.ExternalService.
	err = im.Add("foo", 1, data, false)
	t.Assert(err, NotNil)
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	tmpDir    string
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	configDir string
	api       *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "pct-it-test")

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.configDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestHandleGetInfoMySQL(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	/**
	 * First get MySQL info manually.  This is what GetInfo should do, too.
	 */

	conn := mysql.NewConnection(dsn)
	if err := conn.Connect(1); err != nil {
		t.Fatal(err)
	}
	var hostname, distro, version string
	sql := "SELECT" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	if err := conn.DB().QueryRow(sql).Scan(&hostname, &distro, &version); err != nil {
		t.Fatal(err)
	}

	/**
	 * Now use the instance manager and GetInfo to get MySQL info like API would.
	 */

	// Create an instance manager.
	m := instance.NewManager(s.logger, s.configDir, s.api)
	t.Assert(m, NotNil)

	// API sends Cmd[Service:"it", Cmd:"GetInfo",
	//               Data:proto.ServiceInstance[Service:"mysql",
	//                                          Data:proto.MySQLInstance[]]]
	// Only DSN is needed.  We set Id just to test that it's not changed.
	mysqlIt := &proto.MySQLInstance{
		Id:  9,
		DSN: dsn,
	}
	mysqlData, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)

	serviceIt := &proto.ServiceInstance{
		Service:  "mysql",
		Instance: mysqlData,
	}
	serviceData, err := json.Marshal(serviceIt)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Cmd:     "GetInfo",
		Service: "instance",
		Data:    serviceData,
	}

	reply := m.Handle(cmd)

	got := &proto.MySQLInstance{}
	err = json.Unmarshal(reply.Data, got)
	t.Assert(err, IsNil)

	t.Check(got.Id, Equals, uint(9))        // not changed
	t.Check(got.DSN, Equals, mysqlIt.DSN)   // not changed
	t.Check(got.Hostname, Equals, hostname) // new
	t.Check(got.Distro, Equals, distro)     // new
	t.Check(got.Version, Equals, version)   // new
}
