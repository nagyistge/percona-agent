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

package instance_test

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type RepoTestSuite struct {
	tmpDir        string
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	configDir     string
	api           *mock.API
	instances     proto.Instance
	instancesFile string
	im            *instance.Repo
}

var _ = Suite(&RepoTestSuite{})

func (s *RepoTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "instance-test-")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.logChan = make(chan *proto.LogEntry, 0)
	s.logger = pct.NewLogger(s.logChan, "pct-repo-test")
}

func (s *RepoTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.configDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}

	links := map[string]string{
		"insts": "http://localhost/insts",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
	s.im = instance.NewRepo(s.logger, s.configDir, s.api)
	t.Assert(s.im, NotNil)

	s.instancesFile = filepath.Join(s.configDir, "instances.conf")
	err := test.CopyFile(test.RootDir+"/instance/instances-1.conf", s.instancesFile)
	t.Assert(err, IsNil)

	data, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)

	err = json.Unmarshal(data, &s.instances)
	t.Assert(err, IsNil)
}

func (s *RepoTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *RepoTestSuite) TestInit(t *C) {
	err := s.im.Init()
	t.Assert(err, IsNil)

	tree := s.im.Instances()
	if same, diff := test.IsDeeply(tree, s.instances); !same {
		test.Dump(tree)
		test.Dump(s.instances)
		t.Error(diff)
	}
	t.Assert(len(s.im.List()), Equals, 7)
}

func (s *RepoTestSuite) TestInitDownload(t *C) {
	bin, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)
	s.api.GetData = [][]byte{bin}
	s.api.GetCode = []int{http.StatusOK}

	// Remove our local test config file, so Init will download it and place it there
	err = os.Remove(s.instancesFile)
	t.Assert(err, IsNil)

	err = s.im.Init()
	t.Assert(err, IsNil)

	t.Assert(pct.FileExists(s.instancesFile), Equals, true)
	downloadedFile, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)

	var original, saved *proto.Instance
	err = json.Unmarshal(bin, &original)
	t.Assert(err, IsNil)
	err = json.Unmarshal(downloadedFile, &saved)
	t.Assert(err, IsNil)

	if same, diff := test.IsDeeply(original, saved); !same {
		test.Dump(original)
		test.Dump(saved)
		t.Error(diff)
	}
}

func (s *RepoTestSuite) TestUpdateTreeWrongRoot(t *C) {
	// Init with test data
	err := s.im.Init()
	t.Assert(err, IsNil)

	// Request 2 instance tree copies (using instances-1.conf fixture)
	orig_tree := s.im.Instances()
	tree := s.im.Instances()

	// Make our test tree root instance not an OS type, pick any Subsystem
	tree = tree.Subsystems[0]

	added := make([]proto.Instance, 0)
	deleted := make([]proto.Instance, 0)
	updated := make([]proto.Instance, 0)
	err = s.im.UpdateTree(tree, &added, &deleted, &updated, true)
	t.Assert(err, NotNil)

	// No instance was updated
	t.Assert(len(added), Equals, 0)
	t.Assert(len(deleted), Equals, 0)
	t.Assert(len(updated), Equals, 0)

	// Check if saved instance config was not modified
	savedTreeData, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)
	var savedTree *proto.Instance = nil
	err = json.Unmarshal(savedTreeData, &savedTree)
	t.Assert(err, IsNil)
	if same, diff := test.IsDeeply(&orig_tree, savedTree); !same {
		test.Dump(&orig_tree)
		test.Dump(savedTree)
		t.Error(diff)
	}
}

func (s *RepoTestSuite) TestUpdateTree(t *C) {
	// Init with test data
	err := s.im.Init()
	t.Assert(err, IsNil)

	// Request an instance tree copy (using instances-1.conf fixture)
	tree := s.im.Instances()

	// Lets modify one instance in our test tree copy
	// index 1 corresponds to instance c540346a644b404a9d2ae006122fc5a2
	tree.Subsystems[1].Properties["dsn"] = "other DSN"
	// Remove last element
	_, tree.Subsystems = tree.Subsystems[len(tree.Subsystems)-1], tree.Subsystems[:len(tree.Subsystems)-1]
	// Add new instance
	mysqlIt := &proto.Instance{}
	mysqlIt.Type = "MySQL"
	mysqlIt.Prefix = "mysql"
	mysqlIt.UUID = "27aec282f0e7b25bc4bffdbe4a432a66"
	mysqlIt.Name = "test-mysql"
	mysqlIt.Properties = map[string]string{"dsn": "test/"}
	tree.Subsystems = append(tree.Subsystems, *mysqlIt)

	added := make([]proto.Instance, 0)
	deleted := make([]proto.Instance, 0)
	updated := make([]proto.Instance, 0)
	err = s.im.UpdateTree(tree, &added, &deleted, &updated, true)
	t.Assert(err, IsNil)

	t.Assert(len(added), Equals, 1)
	t.Assert(len(deleted), Equals, 1)
	t.Assert(len(updated), Equals, 2)

	// The new instance
	t.Assert(added[0].UUID, Equals, "27aec282f0e7b25bc4bffdbe4a432a66")
	// The deleted MySQL instance leaf
	t.Assert(deleted[0].UUID, Equals, "67b6ac9eaace265d3dad87663235eba8")
	// The root lost a subsystem
	t.Assert(updated[0].UUID, Equals, "31dd3b7b602849f8871fd3e7acc8c2e3")
	//The updated MySQL instance
	t.Assert(updated[1].UUID, Equals, "c540346a644b404a9d2ae006122fc5a2")
	t.Assert(updated[1].Properties["dsn"], Equals, "other DSN")

	// Check if saved file has the same modified tree structure
	savedTree, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)
	var newTree *proto.Instance = nil
	err = json.Unmarshal(savedTree, &newTree)
	t.Assert(err, IsNil)
	if same, diff := test.IsDeeply(&tree, newTree); !same {
		test.Dump(&tree)
		test.Dump(newTree)
		t.Error(diff)
	}
}

///////////////////////////////////////////////////////////////////////////////
//// Manager test suite
///////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	tmpDir        string
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	configDir     string
	instancesFile string
	api           *mock.API
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
		"insts": "http://localhost/insts",
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
	s.instancesFile = filepath.Join(s.configDir, "instances.conf")
	err := test.CopyFile(test.RootDir+"/instance/instances-1.conf", s.instancesFile)
	t.Assert(err, IsNil)

}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

//// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestHandleGetInfoMySQL(t *C) {
	bin, err := ioutil.ReadFile(s.instancesFile)
	t.Assert(err, IsNil)
	s.api.GetData = [][]byte{bin}
	s.api.GetCode = []int{http.StatusOK}

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
	mrm := mock.NewMrmsMonitor()
	m := instance.NewManager(s.logger, s.configDir, s.api, mrm)
	t.Assert(m, NotNil)

	err = m.Start()
	t.Assert(err, IsNil)

	// API sends Cmd[Service:"instance", Cmd:"GetInfo",
	//               Data:proto.ServiceInstance[Service:"mysql",
	//                                          Data:proto.MySQLInstance[]]]
	// Only DSN is needed.  We set Id just to test that it's not changed.
	mysqlIt := &proto.Instance{}
	mysqlIt.Type = "MySQL"
	mysqlIt.Prefix = "mysql"
	mysqlIt.UUID = "c540346a644b404a9d2ae006122fc5a2"
	mysqlIt.Name = "mysql-bm-cloud-0001"
	mysqlIt.Properties = map[string]string{"dsn": dsn}
	mysqlData, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Cmd:     "GetInfo",
		Service: "instance",
		Data:    mysqlData,
	}

	reply := m.Handle(cmd)

	var got *proto.Instance
	err = json.Unmarshal(reply.Data, &got)
	t.Assert(err, IsNil)

	t.Check(got.Type, Equals, mysqlIt.Type)               // not changed
	t.Check(got.Prefix, Equals, mysqlIt.Prefix)           // not changed
	t.Check(got.UUID, Equals, mysqlIt.UUID)               // not changed
	t.Check(got.Properties["dsn"], Equals, dsn)           // not changed
	t.Check(got.Properties["hostname"], Equals, hostname) // new
	t.Check(got.Properties["distro"], Equals, distro)     // new
	t.Check(got.Properties["version"], Equals, version)   // new
}

func (s *ManagerTestSuite) TestHandleUpdate(t *C) {
	// Create an instance manager.
	mrm := mock.NewMrmsMonitor()
	m := instance.NewManager(s.logger, s.configDir, s.api, mrm)
	t.Assert(m, NotNil)

	osIt := &proto.Instance{}
	osIt.Type = "OS"
	osIt.Prefix = "os"
	osIt.UUID = "916f4c31aaa35d6b867dae9a7f54270d"
	osIt.Name = "os-bm-cloud-0001"
	mysqlIt := &proto.Instance{}
	mysqlIt.Type = "MySQL"
	mysqlIt.Prefix = "mysql"
	mysqlIt.UUID = "c540346a644b404a9d2ae006122fc5a2"
	mysqlIt.Name = "mysql-bm-cloud-0001"
	mysqlIt.Properties = map[string]string{"dsn": dsn}
	osIt.Subsystems = append(osIt.Subsystems, *mysqlIt)

	osData, err := json.Marshal(osIt)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Cmd:     "Update",
		Service: "instance",
		Data:    osData,
	}

	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// Test GetMySQLInstances here because we already have a Repo with instances
	mySQLinsts := m.GetMySQLInstances()
	t.Assert(mySQLinsts, NotNil)
	t.Assert(len(mySQLinsts), Equals, 1)

	t.Assert(mySQLinsts[0].UUID, Equals, "c540346a644b404a9d2ae006122fc5a2")
}

func (s *ManagerTestSuite) TestHandleUpdateNoOS(t *C) {
	// Create an instance manager.
	mrm := mock.NewMrmsMonitor()
	m := instance.NewManager(s.logger, s.configDir, s.api, mrm)
	t.Assert(m, NotNil)

	mysqlIt := &proto.Instance{}
	mysqlIt.Type = "MySQL"
	mysqlIt.Prefix = "mysql"
	mysqlIt.UUID = "c540346a644b404a9d2ae006122fc5a2"
	mysqlIt.Name = "mysql-bm-cloud-0001"
	mysqlIt.Properties = map[string]string{"dsn": dsn}
	mysqlData, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Cmd:     "Update",
		Service: "instance",
		Data:    mysqlData,
	}

	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "Tree instance root is not of 'OS' type and 'os' prefix")
}
