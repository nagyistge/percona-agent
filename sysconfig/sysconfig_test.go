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

package sysconfig_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/data"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/sysconfig"
	"github.com/percona/percona-agent/sysconfig/mysql"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	mockMonitor   *mock.SysconfigMonitor
	factory       *mock.SysconfigMonitorFactory
	tickChan      chan time.Time
	clock         *mock.Clock
	dataChan      chan interface{}
	spool         data.Spooler
	tmpDir        string
	configDir     string
	ir            *instance.Repo
	mysqlInstance proto.Instance
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "sysconfig-manager-test")

	s.mockMonitor = mock.NewSysconfigMonitor()
	s.factory = mock.NewSysconfigMonitorFactory([]sysconfig.Monitor{s.mockMonitor})

	s.tickChan = make(chan time.Time)

	s.dataChan = make(chan interface{}, 1)
	s.spool = mock.NewSpooler(s.dataChan)

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	systemTreeFile := filepath.Join(s.configDir, instance.SYSTEM_TREE_FILE)
	err = test.CopyFile(test.RootDir+"/instance/system-tree-1.json", systemTreeFile)
	t.Assert(err, IsNil)

	links := map[string]string{
		"agent":       "http://localhost/agent",
		"instances":   "http://localhost/instances",
		"system_tree": "http://localhost/systemtree",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
	s.ir = instance.NewRepo(s.logger, s.configDir, s.api)
	t.Assert(s.ir, NotNil)
	err = s.ir.Init()
	t.Assert(err, IsNil)
	s.mysqlInstance, err = s.ir.Get("00000000000000000000000000000003")
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	s.factory.Set([]sysconfig.Monitor{s.mockMonitor})
	s.clock = mock.NewClock()
	glob := filepath.Join(pct.Basedir.Dir("config"), "*")
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopManager(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.ir)
	t.Assert(m, NotNil)

	// It shouldn't have added a tickChan yet.
	if len(s.clock.Added) != 0 {
		t.Error("tickChan not added yet")
	}

	// Write a sysconfig monitor config to disk.
	config := &sysconfig.Config{
		UUID:   s.mysqlInstance.UUID,
		Report: 3600,
		// No monitor-specific config
	}
	pct.Basedir.WriteConfig("sysconfig-mysql-1", config)

	// The agent calls sysconfig.Start() to start manager which starts all monitors.
	err := m.Start()
	t.Assert(err, IsNil)

	// It should not add a tickChan to the clock (this is done in Handle()).
	if ok, diff := IsDeeply(s.clock.Added, []uint{3600}); !ok {
		t.Errorf("Adds tickChan, got %#v", diff)
	}

	// Its status should be "Running".
	status := m.Status()
	t.Check(status["sysconfig"], Equals, "Running")

	// Can't start manager twice.
	err = m.Start()
	t.Check(err, NotNil)

	// Stopping is idempotent.
	err = m.Stop()
	t.Check(err, IsNil)
	err = m.Stop()
	t.Check(err, IsNil)

	status = m.Status()
	t.Check(status["sysconfig"], Equals, "Stopped")
}

func (s *ManagerTestSuite) TestStartStopMonitor(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.ir)
	t.Assert(m, NotNil)

	err := m.Start()
	t.Assert(err, IsNil)

	// Starting a monitor is like starting the manager: it requires
	// a "StartService" cmd and the monitor's config.  This is the
	// config in configDir/db1-mysql-monitor.conf.
	sysconfigConfig := &mysql.Config{
		Config: sysconfig.Config{
			UUID:   s.mysqlInstance.UUID,
			Report: 3600,
		},
	}
	sysconfigConfigData, err := json.Marshal(sysconfigConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		User: "daniel",
		Tool: "sysconfig",
		Cmd:  "StartService",
		Data: sysconfigConfigData,
	}

	// If this were a real monitor, it would decode and set its own config.
	// The mock monitor doesn't have any real config type, so we set it manually.
	s.mockMonitor.SetConfig(sysconfigConfig)

	// The agent calls sysconfig.Handle() with the cmd (for logging and status) and the config data.
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	// The monitor should be running.  The mock monitor returns "Running" if
	// Start() has been called; else it returns "Stopped".
	status := s.mockMonitor.Status()
	if status["monitor"] != "Running" {
		t.Error("Monitor running")
	}

	// There should be a 60s report ticker for the aggregator and a 1s collect ticker
	// for the monitor.
	if ok, diff := IsDeeply(s.clock.Added, []uint{3600}); !ok {
		t.Errorf("Make 3600s ticker for collect interval\n%s", diff)
	}

	// After starting a monitor, sysconfig should write its config to the dir
	// it learned when sysconfig.LoadConfig() was called.  Next time agent starts,
	// it will have sysconfig start the monitor with this config.
	data, err := ioutil.ReadFile(fmt.Sprintf("%s/sysconfig-%s.conf", s.configDir, s.mysqlInstance.UUID))
	t.Check(err, IsNil)
	gotConfig := &mysql.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := IsDeeply(gotConfig, sysconfigConfig); !same {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	/**
	 * Stop the monitor.
	 */

	cmd = &proto.Cmd{
		User: "daniel",
		Tool: "sysconfig",
		Cmd:  "StopService",
		Data: sysconfigConfigData,
	}

	// Handles StopService without error.
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	status = s.mockMonitor.Status()
	if status["monitor"] != "Stopped" {
		t.Error("Monitor stopped")
	}

	// After stopping the monitor, the manager should remove its tickChan.
	if len(s.clock.Removed) != 1 {
		t.Error("Remove's monitor's tickChan from clock")
	}

	// After stopping a monitor, sysconfig should remove its config file so agent
	// doesn't start it on restart.
	file := s.configDir + "/sysconfig-mysql-1.conf"
	if pct.FileExists(file) {
		t.Error("Stopping monitor removes its config; ", file, " exists")
	}

	/**
	 * While we're all setup and working, let's sneak in an unknown cmd test.
	 */

	cmd = &proto.Cmd{
		User: "daniel",
		Tool: "sysconfig",
		Cmd:  "Pontificate",
		Data: sysconfigConfigData,
	}

	// Unknown cmd causes error.
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	if reply.Error == "" {
		t.Fatalf("Unknown Cmd to Handle() causes error")
	}

	/**
	 * Clean up
	 */
	m.Stop()
}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.ir)
	t.Assert(m, NotNil)

	err := m.Start()
	t.Assert(err, IsNil)

	// Start a sysconfig monitor.
	sysconfigConfig := &mysql.Config{
		Config: sysconfig.Config{
			UUID:   s.mysqlInstance.UUID,
			Report: 3600,
		},
	}
	sysconfigConfigData, err := json.Marshal(sysconfigConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		User: "daniel",
		Tool: "sysconfig",
		Cmd:  "StartService",
		Data: sysconfigConfigData,
	}
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")
	s.mockMonitor.SetConfig(sysconfigConfig)

	/**
	 * GetConfig from sysconfig which should return all monitors' configs.
	 */
	cmd = &proto.Cmd{
		Cmd:  "GetConfig",
		Tool: "sysconfig",
	}
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfig := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfig); err != nil {
		t.Fatal(err)
	}
	expectConfig := []proto.AgentConfig{
		{
			Tool:    "sysconfig",
			UUID:    s.mysqlInstance.UUID,
			Config:  string(sysconfigConfigData),
			Running: true,
		},
	}
	if same, diff := IsDeeply(gotConfig, expectConfig); !same {
		Dump(gotConfig)
		t.Error(diff)
	}
}
