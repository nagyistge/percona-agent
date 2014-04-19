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

package sysconfig_test

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sysconfig"
	"github.com/percona/cloud-tools/sysconfig/mysql"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan     chan *proto.LogEntry
	logger      *pct.Logger
	mockMonitor *mock.SysconfigMonitor
	factory     *mock.SysconfigMonitorFactory
	tickChan    chan time.Time
	clock       *mock.Clock
	dataChan    chan interface{}
	spool       data.Spooler
	tmpDir      string
	configDir   string
	im          *instance.Repo
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

	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "im-test"), s.configDir)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	s.factory.Set([]sysconfig.Monitor{s.mockMonitor})
	s.clock = mock.NewClock()
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopManager(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	t.Assert(m, NotNil)

	// It shouldn't have added a tickChan yet.
	if len(s.clock.Added) != 0 {
		t.Error("tickChan not added yet")
	}

	// First the API marshals an sysconfig.Config.
	config := &sysconfig.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		Report:          3600,
		// No monitor-specific config
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Then it sends a StartService cmd with the config data.
	cmd := &proto.Cmd{
		User: "daniel",
		Cmd:  "StartService",
		Data: data,
	}

	// The agent calls sysconfig.Start with the cmd (for logging and status) and the config data.
	err = m.Start(cmd, data)
	if err != nil {
		t.Fatalf("Start manager without error, got %s", err)
	}

	// It should not add a tickChan to the clock (this is done in Handle()).
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{}); !ok {
		t.Errorf("Does not add tickChan, got %#v", diff)
	}

	// Its status should be "Running".
	status := m.Status()
	t.Check(status["sysconfig"], Equals, "Running")

	// Normally, starting an already started service results in a ServiceIsRunningError,
	// but sysconfig is a proxy manager so starting it is a null op.
	err = m.Start(cmd, data)
	if err != nil {
		t.Error("Starting sysconfig manager multiple times doesn't cause error")
	}

	// Stopping the sysconfig manager is also a null op...
	err = m.Stop(cmd)
	if err != nil {
		t.Fatalf("Stop manager without error, got %s", err)
	}

	// ...which is why its status is always "Running".
	status = m.Status()
	t.Check(status["sysconfig"], Equals, "Running")
}

func (s *ManagerTestSuite) TestStartStopMonitor(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	t.Assert(m, NotNil)

	// sysconfig is a proxy manager so it doesn't have its own config file,
	// but agent still calls LoadConfig() because this also tells
	// the manager where to save configs, monitor configs in this case.
	v, err := m.LoadConfig()
	t.Check(v, IsNil)
	t.Check(err, IsNil)

	// Starting a monitor is like starting the manager: it requires
	// a "StartService" cmd and the monitor's config.  This is the
	// config in configDir/db1-mysql-monitor.conf.
	sysconfigConfig := &mysql.Config{
		Config: sysconfig.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
			Report: 3600,
		},
	}
	sysconfigConfigData, err := json.Marshal(sysconfigConfig)
	if err != nil {
		t.Fatal(err)
	}

	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "sysconfig",
		Cmd:     "StartService",
		Data:    sysconfigConfigData,
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
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{3600}); !ok {
		t.Errorf("Make 3600s ticker for collect interval\n%s", diff)
	}

	// After starting a monitor, sysconfig should write its config to the dir
	// it learned when sysconfig.LoadConfig() was called.  Next time agent starts,
	// it will have sysconfig start the monitor with this config.
	data, err := ioutil.ReadFile(s.configDir + "/sysconfig-mysql-1.conf")
	t.Check(err, IsNil)
	gotConfig := &mysql.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := test.IsDeeply(gotConfig, sysconfigConfig); !same {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	/**
	 * Stop the monitor.
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "sysconfig",
		Cmd:     "StopService",
		Data:    sysconfigConfigData,
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
		User:    "daniel",
		Service: "sysconfig",
		Cmd:     "Pontificate",
		Data:    sysconfigConfigData,
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
	m.Stop(cmd)
}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	m := sysconfig.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	t.Assert(m, NotNil)

	v, err := m.LoadConfig()
	t.Check(v, IsNil)
	t.Check(err, IsNil)

	// Start a sysconfig monitor.
	sysconfigConfig := &mysql.Config{
		Config: sysconfig.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
			Report: 3600,
		},
	}
	sysconfigConfigData, err := json.Marshal(sysconfigConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "sysconfig",
		Cmd:     "StartService",
		Data:    sysconfigConfigData,
	}
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")
	s.mockMonitor.SetConfig(sysconfigConfig)

	/**
	 * GetConfig from sysconfig which should return all monitors' configs.
	 */
	cmd = &proto.Cmd{
		Cmd:     "GetConfig",
		Service: "sysconfig",
	}
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Assert(reply.Error, Equals, "")

	configs := make(map[string]string)
	if err := json.Unmarshal(reply.Data, &configs); err != nil {
		t.Fatal(err)
	}
	expect := map[string]string{
		"sysconfig-mysql-1": string(sysconfigConfigData),
	}
	if same, diff := test.IsDeeply(configs, expect); !same {
		test.Dump(configs)
		t.Error(diff)
	}
}
