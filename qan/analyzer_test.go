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

	//. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

type AnalyzerTestSuite struct {
	nullmysql     *mock.NullMySQL
	mrmsMonitor   *mock.MrmsMonitor
	iter          *mock.Iter
	spool         *mock.Spooler
	clock         *mock.Clock
	api           *mock.API
	worker        *mock.QanWorker
	restartChan   chan bool
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	intervalChan  chan *qan.Interval
	dataChan      chan interface{}
	tmpDir        string
	configDir     string
	im            *instance.Repo
	mysqlInstance proto.ServiceInstance
	config        qan.Config
}

var _ = Suite(&AnalyzerTestSuite{})

func (s *AnalyzerTestSuite) SetUpSuite(t *C) {
	s.nullmysql = mock.NewNullMySQL()
	s.mrmsMonitor = mock.NewMrmsMonitor()

	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	s.intervalChan = make(chan *qan.Interval, 1)

	s.iter = mock.NewIter(s.intervalChan)

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

	s.worker = mock.NewQanWorker()
	s.restartChan = make(chan bool, 1)
	s.config = qan.Config{
		ServiceInstance: s.mysqlInstance,
		CollectFrom:     "slowlog",
		Interval:        60,
		WorkerRunTime:   60,
		Start: []mysql.Query{
			mysql.Query{Set: "SELECT 'start'"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SELECT 'stop'"},
		},
	}
}

func (s *AnalyzerTestSuite) SetUpTest(t *C) {
	s.nullmysql.Reset()
	s.clock = mock.NewClock()
	if err := test.ClearDir(s.configDir, "*"); err != nil {
		t.Fatal(err)
	}
}

func (s *AnalyzerTestSuite) TearDownTest(t *C) {
}

func (s *AnalyzerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *AnalyzerTestSuite) TestRunMockWorker(t *C) {
	a := qan.NewRealAnalyzer(
		pct.NewLogger(s.logChan, "qan-analyzer"),
		s.config,
		s.iter,
		s.nullmysql,
		s.restartChan,
		s.worker,
		s.clock,
		s.spool,
	)

	err := a.Start()
	t.Assert(err, IsNil)
	test.WaitStatus(1, a, "qan-analyzer", "Idle")

	// No interval yet, so work should not have one.
	t.Check(s.worker.Interval, IsNil)

	// Send an interval. The analyzer runs the worker with it.
	now := time.Now()
	i := &qan.Interval{
		Number:      1,
		StartTime:   now,
		StopTime:    now.Add(1 * time.Minute),
		Filename:    "slow.log",
		StartOffset: 0,
		EndOffset:   999,
	}
	s.intervalChan <- i

	if !test.WaitState(s.worker.SetupChan) {
		t.Fatal("Timeout waiting for <-s.worker.SetupChan")
	}

	t.Check(s.worker.Interval, DeepEquals, i)

	if !test.WaitState(s.worker.RunChan) {
		t.Fatal("Timeout waiting for <-s.worker.SetupChan")
	}

	if !test.WaitState(s.worker.CleanupChan) {
		t.Fatal("Timeout waiting for <-s.worker.SetupChan")
	}

	err = a.Stop()
	t.Assert(err, IsNil)
	test.WaitStatus(1, a, "qan-analyzer", "Stopped")

	t.Check(a.String(), Equals, "qan-analyzer")
}

func (s *AnalyzerTestSuite) TestStartWithConfig(t *C) {
	/*
		start := []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		}
		stop := []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
		}

		start = []mysql.Query{
			mysql.Query{Verify: "performance_schema", Expect: "1"},
			mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'"},
			mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'"},
			mysql.Query{Set: "TRUNCATE performance_schema.events_statements_summary_by_digest"},
		}
		stop = []mysql.Query{
			mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'NO' WHERE NAME = 'statements_digest'"},
			mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'NO', TIMED = 'NO' WHERE NAME LIKE 'statement/sql/%'"},
		}
	*/
}
func (s *AnalyzerTestSuite) TestStartServiceFast(t *C) {
	/**
	 * Like TestStartService but we simulate the next tick being 3m away
	 * (mock.clock.Eta = 180) so that run() sends the first tick on the
	 * tick chan, causing the first interval to start immediately.
	 */
	/*
		s.clock.Eta = 180
		defer func() { s.clock.Eta = 0 }()

		mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
		a := mock.NewQanAnalyzer()
		f := mock.NewQanAnalyzerFactory(a)
		m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
		//m := qan.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
		t.Assert(m, NotNil)

		config := &qan.Config{
			ServiceInstance: s.mysqlInstance,
			Start: []mysql.Query{
				mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			},
			Stop: []mysql.Query{
				mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			},
			Interval:       300,        // 5 min
			MaxSlowLogSize: 1073741824, // 1 GiB
			MaxWorkers:     1,
			WorkerRunTime:  600, // 10 min
			CollectFrom:    "slowlog",
		}
		now := time.Now()
		qanConfig, _ := json.Marshal(config)
		cmd := &proto.Cmd{
			User:      "daniel",
			Ts:        now,
			AgentUuid: "123",
			Service:   "qan",
			Cmd:       "StartService",
			Data:      qanConfig,
		}
		reply := m.Handle(cmd)
		t.Assert(reply.Error, Equals, "")
		test.WaitStatus(1, m, "qan-parser", "Starting")
		//tickChan := s.iterFactory.TickChans[s.iter]
		tickChan := make(chan time.Time)
		t.Assert(tickChan, NotNil)

		// run() should prime the tickChan with the 1st tick immediately.  This makes
		// the interval iter start the interval immediately.  Then run() continues
		// waiting for the iter to send an interval which happens when the real ticker
		// (the clock) sends the 2nd tick which is synced to the interval, thus ending
		// the first interval started by run() and starting the 2nd interval as normal.
		var tick time.Time
		select {
		case tick = <-tickChan:
		case <-time.After(1 * time.Second):
		}
		t.Assert(tick.IsZero(), Not(Equals), true)

		status := m.Status()
		t.Check(status["qan-next-interval"], Equals, "180.0s")

		// Stop QAN.
		cmd = &proto.Cmd{
			User:      "daniel",
			Ts:        now,
			AgentUuid: "123",
			Service:   "",
			Cmd:       "StopService",
		}
		reply = m.Handle(cmd)
		t.Assert(reply.Error, Equals, "")
	*/
}

func (s *AnalyzerTestSuite) TestMySQLRestart(t *C) {

	/**
	 * Create and start manager.
	 */
	/*
	   	mockConn := mock.NewNullMySQL()
	   	mockConnFactory := &mock.ConnectionFactory{
	   		Conn: mockConn,
	   	}
	   	a := mock.NewQanAnalyzer()
	   	f := mock.NewQanAnalyzerFactory(a)
	   	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	   	//m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	   	t.Assert(m, NotNil)

	   	// Create the qan config.
	   	tmpFile := fmt.Sprintf("/tmp/qan_test.TestStartService.%d", os.Getpid())
	   	defer func() { os.Remove(tmpFile) }()
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

	   	// Create the StartService cmd which contains the qan config.
	   	now := time.Now()
	   	qanConfig, _ := json.Marshal(config)
	   	cmd := &proto.Cmd{
	   		Ts:        now,
	   		AgentUuid: "123",
	   		Service:   "agent",
	   		Cmd:       "StartService",
	   		Data:      qanConfig,
	   	}

	   	// Have the service manager start the qa service
	   	reply := m.Handle(cmd)

	   	// It should start without error.
	   	t.Assert(reply.Error, Equals, "")

	   	// And status should be "Running" and "Idle".
	   	test.WaitStatus(1, m, "qan-parser", "Idle (0 of 2 running)")
	   	status := m.Status()
	   	t.Check(status["qan"], Equals, "Running")
	   	t.Check(status["qan-parser"], Equals, "Idle (0 of 2 running)")

	   	// Stop QAN when we are done
	   	cmd = &proto.Cmd{
	   		Ts:        now,
	   		AgentUuid: "123",
	   		Service:   "",
	   		Cmd:       "StopService",
	   	}
	   	defer m.Handle(cmd)

	   	 // QAN should configure mysql at startup

	   	expectedQueries := []mysql.Query{
	   		mysql.Query{
	   			Set:    "SET GLOBAL slow_query_log=OFF",
	   			Verify: "",
	   			Expect: "",
	   		},
	   		mysql.Query{
	   			Set:    "SET GLOBAL long_query_time=0.123",
	   			Verify: "",
	   			Expect: "",
	   		},
	   		mysql.Query{
	   			Set:    "SET GLOBAL slow_query_log=ON",
	   			Verify: "",
	   			Expect: "",
	   		},
	   	}
	   	var gotQueries []mysql.Query

	   	gotQueries = mockConn.GetSet()
	   	t.Assert(gotQueries, DeepEquals, expectedQueries, Commentf("QAN didn't configure MySQL on startup"))

	   	// QAN should also check periodically if MySQL was restarted
	   	 // if so then it should configure it again
	   	gotQueries = nil
	   	mockConn.Reset()
	   	s.mrmsMonitor.SimulateMySQLRestart()
	   	timeout := time.After(1 * time.Second)
	   LOOP:
	   	for {
	   		select {
	   		case <-timeout:
	   			break LOOP
	   		default:
	   			gotQueries = mockConn.GetSet()
	   			if gotQueries != nil {
	   				break LOOP
	   			}
	   		}
	   	}
	   	t.Assert(gotQueries, DeepEquals, expectedQueries, Commentf("MySQL was restarted, but QAN didn't reconfigure MySQL"))
	*/
}

func (s *AnalyzerTestSuite) TestRecoverWorkerPanic(t *C) {
	/*
			// Create and start manager with mock workers.
			w1StopChan := make(chan bool)
			w1 := mock.NewQanWorker("qan-worker-1", w1StopChan, nil, nil, true)
			f := mock.NewQanWorkerFactory([]*mock.QanWorker{w1})
			mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
			a := mock.NewQanAnalyzer()
			f := mock.NewQanAnalyzerFactory(a)
			m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
			//m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, f, s.spool, s.im, s.mrmsMonitor)
			t.Assert(m, NotNil)

			config := &qan.Config{
				ServiceInstance: s.mysqlInstance,
				MaxSlowLogSize:  1000,
				MaxWorkers:      2,
				Interval:        60,
				WorkerRunTime:   60,
				Start: []mysql.Query{
					mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
				},
				Stop: []mysql.Query{
					mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
				},
				CollectFrom: "slowlog",
			}
			qanConfig, _ := json.Marshal(config)
			cmd := &proto.Cmd{
				Ts:   time.Now(),
				Cmd:  "StartService",
				Data: qanConfig,
			}
			reply := m.Handle(cmd)
			t.Assert(reply.Error, Equals, "")

			test.WaitStatusPrefix(1, m, "qan-parser", "Idle")
			test.DrainLogChan(s.logChan)

			// Start mock worker.  All it does is panic, much like fipar.
			now := time.Now()
			i1 := &qan.Interval{
				Filename:    "slow.log",
				StartOffset: 0,
				EndOffset:   100,
				StartTime:   now,
				StopTime:    now,
			}
			s.intervalChan <- i1
			<-w1.Running() // wait for manager to run worker

			// For now, worker panic only results in error to log.
			var gotError *proto.LogEntry
			timeout := time.After(200 * time.Millisecond)
		GET_LOG:
			for {
				select {
				case l := <-s.logChan:
					if l.Level == 3 && strings.HasPrefix(l.Msg, "QAN worker for interval 0 slow.log") {
						gotError = l
						break GET_LOG
					}
				case <-timeout:
					break GET_LOG
				}
			}
			t.Check(gotError, NotNil)

			// Stop manager
			reply = m.Handle(&proto.Cmd{Cmd: "StopService"})
			t.Assert(reply.Error, Equals, "")
	*/
}
