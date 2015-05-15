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

package qan_test

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto/v1"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

type ManagerTestSuite struct {
	nullmysql    *mock.NullMySQL
	mrmsMonitor  *mock.MrmsMonitor
	logChan      chan *proto.LogEntry
	logger       *pct.Logger
	intervalChan chan *qan.Interval
	dataChan     chan interface{}
	spool        *mock.Spooler
	//workerFactory qan.WorkerFactory
	clock         *mock.Clock
	tmpDir        string
	configDir     string
	im            *instance.Repo
	mysqlInstance proto.ServiceInstance
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {

	s.nullmysql = mock.NewNullMySQL()
	s.mrmsMonitor = mock.NewMrmsMonitor()

	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	s.dataChan = make(chan interface{}, 2)
	s.spool = mock.NewSpooler(s.dataChan)

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "im-test"), s.configDir, s.api)
	data, err := json.Marshal(&proto.MySQLInstance{
		Hostname: "bm-cloud-db01",
		Alias:    "db01",
		DSN:      "user:pass@tcp/",
	})
	t.Assert(err, IsNil)
	s.im.Add("mysql", 1, data, false)
	s.mysqlInstance = proto.ServiceInstance{Service: "mysql", InstanceId: 1}

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	s.nullmysql.Reset()
	s.clock = mock.NewClock()
	if err := test.ClearDir(pct.Basedir.Dir("config"), "*"); err != nil {
		t.Fatal(err)
	}
}

func (s *ManagerTestSuite) TearDownTest(t *C) {
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

/*
	Interface tests
*/

func (s *ManagerTestSuite) TestStarNoConfig(t *C) {
	// Make a qan.Manager with mock factories.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer()
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)

	// qan.Manager should be able to start without a qan.conf, i.e. no analyzer.
	err := m.Start()
	t.Check(err, IsNil)

	// Wait for qan.Manager.Start() to finish.
	test.WaitStatus(1, m, "qan", "Running")

	// No analyzer is configured, so the mock analyzer should not be started.
	select {
	case <-a.StartChan:
		t.Error("Analyzer.Start() called")
	default:
	}

	// And the mock analyzer's status should not be reported.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)

	// No analyzer is configured, so the mock analyzer should not be stop.
	select {
	case <-a.StartChan:
		t.Error("Analyzer.Stop() called")
	default:
	}
}

func (s *ManagerTestSuite) TestStartWithConfig(t *C) {
	for _, analyzerType := range []string{"slowlog", "perfschema"} {
		// Make a qan.Manager with mock factories.
		mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
		a := mock.NewQanAnalyzer()
		f := mock.NewQanAnalyzerFactory(a)
		m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
		t.Assert(m, NotNil)

		// Write a realistic qan.conf config to disk.
		config := qan.Config{
			ServiceInstance: s.mysqlInstance,
			CollectFrom:     analyzerType,
			Interval:        300,
			MaxWorkers:      1,
			WorkerRunTime:   600,
			Start: []mysql.Query{
				mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
				mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
				mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
			},
			Stop: []mysql.Query{
				mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
				mysql.Query{Set: "SET GLOBAL long_query_time=10"},
			},
		}
		err := pct.Basedir.WriteConfig("qan", &config)
		t.Assert(err, IsNil)

		// qan.Start() reads qan.conf from disk and starts an analyzer for it.
		err = m.Start()
		t.Check(err, IsNil)

		// Wait until qan.Start() calls analyzer.Start().
		if !test.WaitState(a.StartChan) {
			t.Fatal("Timeout waiting for <-a.StartChan")
		}

		// After starting, the manager's status should be Running and the analyzer's
		// status should be reported too.
		status := m.Status()
		t.Check(status["qan"], Equals, "Running")
		t.Check(status["qan-analyzer"], Equals, "ok")

		// Check the args passed by the manager to the analyzer factory.
		if len(f.Args) == 0 {
			t.Error("len(f.Args) == 0, expected 1")
		} else {
			t.Check(f.Args, HasLen, 1)
			t.Check(f.Args[0].Config, DeepEquals, config)
			t.Check(f.Args[0].Name, Equals, "qan-analyzer")
		}

		// qan.Stop() stops the analyzer and leaves qan.conf on disk.
		err = m.Stop()
		t.Assert(err, IsNil)

		// Wait until qan.Stop() calls analyzer.Stop().
		if !test.WaitState(a.StopChan) {
			t.Fatal("Timeout waiting for <-a.StopChan")
		}

		// qan.conf still exists after qan.Stop().
		t.Check(test.FileExists(pct.Basedir.ConfigFile("qan")), Equals, true)

		// The analyzer is no longer reported in the status because it was stopped
		// and removed when the manager was stopped.
		status = m.Status()
		t.Check(status["qan"], Equals, "Stopped")
	}
}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	// Make a qan.Manager with mock factories.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer()
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)

	// Write a realistic qan.conf config to disk.
	config := qan.Config{
		ServiceInstance: s.mysqlInstance,
		CollectFrom:     "slowlog",
		Interval:        300,
		MaxWorkers:      1,
		WorkerRunTime:   600,
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
		},
	}
	err := pct.Basedir.WriteConfig("qan", &config)
	t.Assert(err, IsNil)

	qanConfig, err := json.Marshal(config)
	t.Assert(err, IsNil)

	// Start the manager and analyzer.
	err = m.Start()
	t.Check(err, IsNil)
	test.WaitStatus(1, m, "qan", "Running")

	// Get the manager config which should be just the analyzer config.
	got, errs := m.GetConfig()
	t.Assert(errs, HasLen, 0)
	t.Assert(got, HasLen, 1)
	expect := []proto.AgentConfig{
		{
			InternalService: "qan",
			Config:          string(qanConfig),
			Running:         true,
		},
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) TestValidateConfig(t *C) {
	config := qan.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.123"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        2,
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "slowlog",
	}
	err := qan.ValidateConfig(&config)
	t.Check(err, IsNil)

	config = qan.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.123"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        0,
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "slowlog",
	}
	err = qan.ValidateConfig(&config)
	t.Check(err, NotNil)

	// CollectFrom is empty in old versions; it should default to "slowlog".
	config = qan.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        0,
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "",
	}
	err = qan.ValidateConfig(&config)
	t.Check(err, NotNil)
	t.Check(config.CollectFrom, Equals, "slowlog")
}

/*
	Handler tests
*/

func (s *ManagerTestSuite) TestStartService(t *C) {
	// Make and start a qan.Manager with mock factories, no analyzer yet.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer()
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Check(err, IsNil)
	test.WaitStatus(1, m, "qan", "Running")

	// Create the qan config.
	config := &qan.Config{
		ServiceInstance: s.mysqlInstance,
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.123"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        2,
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "slowlog",
	}

	// Send a StartService cmd with the qan config to start an analyzer.
	now := time.Now()
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUuid: "123",
		Service:   "agent",
		Cmd:       "StartService",
		Data:      qanConfig,
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// The manager writes the qan config to disk.
	data, err := ioutil.ReadFile(pct.Basedir.ConfigFile("qan"))
	t.Check(err, IsNil)
	gotConfig := &qan.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := IsDeeply(gotConfig, config); !same {
		Dump(gotConfig)
		t.Error(diff)
	}

	// Now the manager and analyzer should be running.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-analyzer"], Equals, "ok")

	// Try to start the same analyzer again. It results in an error because
	// double tooling is not allowed.
	reply = m.Handle(cmd)
	t.Check(reply.Error, Equals, "qan-analyzer service is running")

	// Send a StopService cmd to stop the analyzer.
	// todo-1.1: send Data with analyzer instance to stop.
	now = time.Now()
	cmd = &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUuid: "123",
		Service:   "qan",
		Cmd:       "StopService",
	}
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// Now the manager is still running, but the analyzer is not.
	status = m.Status()
	t.Check(status["qan"], Equals, "Running")

	// And the manager has removed the qan config from disk so next time
	// the agent starts the analyzer is not started.
	t.Check(test.FileExists(pct.Basedir.ConfigFile("qan")), Equals, false)

	// StopService should be idempotent, so send it again and expect no error.
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) TestBadCmd(t *C) {
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer()
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Check(err, IsNil)
	defer m.Stop()
	test.WaitStatus(1, m, "qan", "Running")
	cmd := &proto.Cmd{
		User:      "daniel",
		Ts:        time.Now(),
		AgentUuid: "123",
		Service:   "qan",
		Cmd:       "foo", // bad cmd
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "Unknown command: foo")
}
