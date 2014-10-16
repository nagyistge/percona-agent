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

package qan_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/mysql-log-parser/test"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/qan/"

/////////////////////////////////////////////////////////////////////////////
// SlowLogWorker test suite
/////////////////////////////////////////////////////////////////////////////

type SlowLogWorkerTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&SlowLogWorkerTestSuite{})

func (s *SlowLogWorkerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
}

func (s *SlowLogWorkerTestSuite) RunSlowLogWorker(job *qan.Job) (*qan.Result, error) {
	w := qan.NewSlowLogWorker(s.logger, "qan-worker-1")
	return w.Run(job)
}

func (s *SlowLogWorkerTestSuite) TestWorkerSlow001(t *C) {
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		EndOffset:      524,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: true,
	}
	got, err := s.RunSlowLogWorker(job)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadMmReport(sample+"slow001.json", expect)
	if ok, diff := test.IsDeeply(got, expect); !ok {
		test.Dump(got)
		t.Error(diff)
	}
}

func (s *SlowLogWorkerTestSuite) TestWorkerSlow001NoExamples(t *C) {
	job := &qan.Job{
		Id:             "99",
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		EndOffset:      524,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: false,
	}
	w := qan.NewSlowLogWorker(s.logger, "qan-worker-1")
	got, _ := w.Run(job)
	expect := &qan.Result{}
	if err := test.LoadMmReport(sample+"slow001-no-examples.json", expect); err != nil {
		t.Fatal(err)
	}

	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}

	// SlowLogWorker should be able to report its name and status.
	t.Check(w.Name(), Equals, "qan-worker-1")
	t.Check(w.Status(), Equals, "Done job "+job.Id)
}

func (s *SlowLogWorkerTestSuite) TestWorkerSlow001Half(t *C) {
	// This tests that the worker will stop processing events before
	// the end of the slow log file.  358 is the last byte of the first
	// (of 2) events.
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		EndOffset:      358,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: true,
	}
	got, err := s.RunSlowLogWorker(job)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadMmReport(sample+"slow001-half.json", expect); err != nil {
		t.Fatal(err)
	}
	if ok, diff := test.IsDeeply(got, expect); !ok {
		test.Dump(got)
		t.Error(diff)
	}
}

func (s *SlowLogWorkerTestSuite) TestSlowLogWorkerSlow001Resume(t *C) {
	// This tests that the worker will resume processing events from
	// somewhere in the slow log file.  359 is the first byte of the
	// second (of 2) events.
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    359,
		EndOffset:      524,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: true,
	}
	got, err := s.RunSlowLogWorker(job)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadMmReport(sample+"slow001-resume.json", expect)
	if ok, diff := test.IsDeeply(got, expect); !ok {
		test.Dump(got)
		t.Error(diff)
	}
}

func (s *SlowLogWorkerTestSuite) TestWorkerSlow011(t *C) {
	// Percona Server rate limit
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow011.log",
		StartOffset:    0,
		EndOffset:      3000,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: true,
	}
	w := qan.NewSlowLogWorker(s.logger, "qan-worker-1")
	got, _ := w.Run(job)

	expect := &qan.Result{}
	if err := test.LoadMmReport(sample+"slow011.json", expect); err != nil {
		t.Fatal(err)
	}

	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	dsn           string
	realmysql     *mysql.Connection
	nullmysql     *mock.NullMySQL
	mrmsMonitor   *mock.MrmsMonitor
	reset         []mysql.Query
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	intervalChan  chan *qan.Interval
	iter          qan.IntervalIter
	iterFactory   *mock.IntervalIterFactory
	dataChan      chan interface{}
	spool         *mock.Spooler
	workerFactory qan.WorkerFactory
	clock         *mock.Clock
	tmpDir        string
	configDir     string
	im            *instance.Repo
	mysqlInstance proto.ServiceInstance
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	s.realmysql = mysql.NewConnection(s.dsn)
	if err := s.realmysql.Connect(1); err != nil {
		t.Fatal(err)
	}
	s.reset = []mysql.Query{
		mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
		mysql.Query{Set: "SET GLOBAL long_query_time=10"},
	}

	s.nullmysql = mock.NewNullMySQL()
	s.mrmsMonitor = mock.NewMrmsMonitor()

	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	s.intervalChan = make(chan *qan.Interval, 1)
	s.iter = mock.NewMockIntervalIter(s.intervalChan)
	s.iterFactory = &mock.IntervalIterFactory{
		Iters:     []qan.IntervalIter{s.iter},
		TickChans: make(map[qan.IntervalIter]chan time.Time),
	}

	s.dataChan = make(chan interface{}, 2)
	s.spool = mock.NewSpooler(s.dataChan)
	s.workerFactory = &qan.RealWorkerFactory{}

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "im-test"), s.configDir, s.api)
	data, err := json.Marshal(&proto.MySQLInstance{
		Hostname: "db1",
		DSN:      s.dsn,
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
	err := s.realmysql.Set(s.reset)
	if err != nil {
		t.Fatal(err)
	}
	s.nullmysql.Reset()
	s.clock = mock.NewClock()

	s.iterFactory.Iters = []qan.IntervalIter{s.iter}
	s.iterFactory.TickChans = make(map[qan.IntervalIter]chan time.Time)
	s.iterFactory.Reset()
}

func (s *ManagerTestSuite) TearDownTest(t *C) {
	err := s.realmysql.Set(s.reset)
	if err != nil {
		t.Fatal(err)
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartService(t *C) {

	/**
	 * Create and start manager.
	 */

	m := qan.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
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
		User:      "daniel",
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

	// It should write the config to disk.
	data, err := ioutil.ReadFile(pct.Basedir.ConfigFile("qan"))
	t.Check(err, IsNil)
	gotConfig := &qan.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := test.IsDeeply(gotConfig, config); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}

	// And status should be "Running" and "Idle".
	test.WaitStatus(1, m, "qan-parser", "Idle (0 of 2 running)")
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-parser"], Equals, "Idle (0 of 2 running)")

	// It should have enabled the slow log.
	slowLog := s.realmysql.GetGlobalVarNumber("slow_query_log")
	t.Assert(slowLog, Equals, float64(1))

	longQueryTime := s.realmysql.GetGlobalVarNumber("long_query_time")
	t.Assert(longQueryTime, Equals, 0.123)

	// Starting an already started service should result in a ServiceIsRunningError.
	reply = m.Handle(cmd)
	t.Check(reply.Error, Not(Equals), "")

	// It should add a tickChan for the interval iter
	t.Check(s.clock.Added, HasLen, 1)
	t.Check(s.clock.Removed, HasLen, 0)

	/**
	 * Have manager run a worker, parse, and send data.
	 */

	interv := &qan.Interval{
		Filename:    testlog.Sample + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- interv

	v := test.WaitData(s.dataChan)
	t.Assert(v, HasLen, 1)
	report := v[0].(*qan.Report)

	got := &qan.Result{
		StopOffset: report.StopOffset,
		Global:     report.Global,
		Classes:    report.Class,
	}
	expect := &qan.Result{}
	if err := test.LoadMmReport(sample+"slow001.json", expect); err != nil {
		t.Fatal(err)
	}
	if ok, diff := test.IsDeeply(got, expect); !ok {
		test.Dump(got)
		t.Error(diff)
	}

	/**
	 * Send StopService cmd to stop qan/qan-parser.
	 */

	now = time.Now()
	cmd = &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUuid: "123",
		Service:   "agent",
		Cmd:       "StopService",
	}

	// Have the service manager start the qa service
	reply = m.Handle(cmd)

	// It should start without error.
	t.Assert(reply.Error, Equals, "")

	// It should disable the slow log.
	slowLog = s.realmysql.GetGlobalVarNumber("slow_query_log")
	t.Assert(slowLog, Equals, float64(0))

	longQueryTime = s.realmysql.GetGlobalVarNumber("long_query_time")
	t.Assert(longQueryTime, Equals, 10.0)

	// It should remove the tickChan (and not have added others).
	t.Check(s.clock.Added, HasLen, 1)
	t.Check(s.clock.Removed, HasLen, 1)

	// qan still running, but qan-parser stopped.
	test.WaitStatus(1, m, "qan-parser", "Stopped")
	status = m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-parser"], Equals, "Stopped")
}

func (s *ManagerTestSuite) TestStartServiceFast(t *C) {
	/**
	 * Like TestStartService but we simulate the next tick being 3m away
	 * (mock.clock.Eta = 180) so that run() sends the first tick on the
	 * tick chan, causing the first interval to start immediately.
	 */

	s.clock.Eta = 180
	defer func() { s.clock.Eta = 0 }()

	m := qan.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
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
	tickChan := s.iterFactory.TickChans[s.iter]
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
}

func (s *ManagerTestSuite) TestMySQLRestart(t *C) {

	/**
	 * Create and start manager.
	 */

	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
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

	/**
	 * QAN should configure mysql at startup
	 */

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

	/**
	 * QAN should also check periodically if MySQL was restarted
	 * if so then it should configure it again
	 */
	gotQueries = nil
	mockConn.Set(nil)
	m.GetRestartChan() <- true // imitate mysql restart
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
}

func (s *ManagerTestSuite) TestRotateAndRemoveSlowLog(t *C) {

	// Clean up files that may interfere with test.
	slowlog := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}

	/**
	 * slow006.log is 2200 bytes large.  Rotation happens when manager
	 * see interval.EndOffset >= MaxSlowLogSize.  So we'll use these
	 * intervals,
	 *      0 -  736
	 *    736 - 1833
	 *   1833 - 2200
	 * and set MaxSlowLogSize=1000 which should make manager rotate the log
	 * after the 2nd interval.  When manager rotates log, it 1) renames log
	 * to NAME-TS where NAME is the original name and TS is the current Unix
	 * timestamp (UTC); and 2) it sets interval.StopOff = file size of NAME-TS
	 * to finish parsing the log.  Therefore, results for 2nd interval should
	 * include our 3rd interval. -- Manager also calls Start and Stop so the
	 * nullmysql conn should record the queries being set.
	 */

	// See TestStartService() for description of these startup tasks.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	if m == nil {
		t.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		ServiceInstance:   s.mysqlInstance,
		Interval:          300,
		MaxSlowLogSize:    1000, // <-- HERE
		RemoveOldSlowLogs: true, // <-- HERE too
		ExampleQueries:    false,
		MaxWorkers:        2,
		WorkerRunTime:     600,
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
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

	// Make copy of slow log because test will mv/rename it.
	cp := exec.Command("cp", testlog.Sample+slowlog, "/tmp/"+slowlog)
	cp.Run()

	// First interval: 0 - 736
	now := time.Now()
	i1 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 0,
		EndOffset:   736,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i1
	resultData := <-s.dataChan
	report := *resultData.(*qan.Report)
	if report.Global.TotalQueries != 2 {
		t.Error("First interval has 2 queries, got ", report.Global.TotalQueries)
	}
	if report.Global.UniqueQueries != 1 {
		t.Error("First interval has 1 unique query, got ", report.Global.UniqueQueries)
	}

	// Second interval: 736 - 1833, but will actually go to end: 2200, if not
	// the next two test will fail.
	i2 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 736,
		EndOffset:   1833,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i2
	resultData = <-s.dataChan
	report = *resultData.(*qan.Report)
	if report.Global.TotalQueries != 4 {
		t.Error("Second interval has 2 queries, got ", report.Global.TotalQueries)
	}
	if report.Global.UniqueQueries != 2 {
		t.Error("Second interval has 2 unique queries, got ", report.Global.UniqueQueries)
	}

	test.WaitStatus(1, m, "qan-parser", "Idle (0 of 2 running)")

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		t.Error("/tmp/" + slowlog + " no longer exists")
	}

	// The original slow log should have been renamed to slow006-TS, parsed, and removed.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 0 {
		t.Errorf("Old slow log removed, got %+v", files)
	}
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	// https://jira.percona.com/browse/PCT-466
	// Old slow log removed but space not freed in filesystem
	pid := fmt.Sprintf("%d", os.Getpid())
	out, err := exec.Command("lsof", "-p", pid).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "/tmp/"+slowlog+"-") {
		t.Logf("%s\n", string(out))
		t.Error("Old slow log removed but not freed in filesystem (PCT-466)")
	}

	// Stop manager
	reply = m.Handle(&proto.Cmd{Cmd: "StopService"})
	t.Assert(reply.Error, Equals, "")
}

func (s *ManagerTestSuite) TestRotateSlowLog(t *C) {

	// Same as TestRotateAndRemoveSlowLog, but with qan.Config.RemoveOldSlowLogs=false
	// and testing that Start and Stop queries were executed.

	slowlog := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}

	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	if m == nil {
		t.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		ServiceInstance:   s.mysqlInstance,
		Interval:          300,
		MaxSlowLogSize:    1000,
		RemoveOldSlowLogs: false, // <-- HERE
		ExampleQueries:    false,
		MaxWorkers:        2,
		WorkerRunTime:     600,
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
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
	s.nullmysql.Reset()
	cp := exec.Command("cp", testlog.Sample+slowlog, "/tmp/"+slowlog)
	cp.Run()

	// First interval: 0 - 736
	now := time.Now()
	i1 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 0,
		EndOffset:   736,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i1
	resultData := <-s.dataChan
	report := *resultData.(*qan.Report)
	if report.Global.TotalQueries != 2 {
		t.Error("First interval has 2 queries, got ", report.Global.TotalQueries)
	}
	if report.Global.UniqueQueries != 1 {
		t.Error("First interval has 1 unique query, got ", report.Global.UniqueQueries)
	}

	// Second interval: 736 - 1833, but will actually go to end: 2200, if not
	// the next two test will fail.
	i2 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 736,
		EndOffset:   1833,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i2
	resultData = <-s.dataChan
	report = *resultData.(*qan.Report)
	if report.Global.TotalQueries != 4 {
		t.Error("Second interval has 2 queries, got ", report.Global.TotalQueries)
	}
	if report.Global.UniqueQueries != 2 {
		t.Error("Second interval has 2 unique queries, got ", report.Global.UniqueQueries)
	}

	test.WaitStatus(1, m, "qan-parser", "Idle (0 of 2 running)")

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		t.Error("/tmp/" + slowlog + " no longer exists")
	}

	// The original slow log should NOT have been removed.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 1 {
		t.Errorf("Old slow log not removed, got %+v", files)
	}
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	expect := []mysql.Query{}
	for _, q := range config.Stop {
		expect = append(expect, q)
	}
	for _, q := range config.Start {
		expect = append(expect, q)
	}
	if same, diff := test.IsDeeply(s.nullmysql.GetSet(), expect); !same {
		t.Logf("%+v", s.nullmysql.GetSet())
		t.Logf("%+v", expect)
		t.Error(diff)
	}

	// Stop manager
	reply = m.Handle(&proto.Cmd{Cmd: "StopService"})
	t.Assert(reply.Error, Equals, "")
}

func (s *ManagerTestSuite) TestWaitRemoveSlowLog(t *C) {

	// Same as TestRotateAndRemoveSlowLog, but we use mock workers so we can
	// test that slow log is not removed until previous workers are done.
	// Mock worker factory will return our mock workers when manager calls Make().
	w1StopChan := make(chan bool)
	w1 := mock.NewQanWorker("qan-worker-1", w1StopChan, nil, nil, false)

	w2StopChan := make(chan bool)
	w2 := mock.NewQanWorker("qan-worker-2", w2StopChan, nil, nil, false)

	// Let's take this time to also test that MaxWorkers is enforced.
	w3 := mock.NewQanWorker("qan-worker-3", nil, nil, nil, false)

	f := mock.NewQanWorkerFactory([]*mock.QanWorker{w1, w2, w3})

	// Clean up files that may interfere with test.  Then copy the test log.
	slowlog := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}
	cp := exec.Command("cp", testlog.Sample+slowlog, "/tmp/"+slowlog)
	cp.Run()

	// Create and start manager with mock workers.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, f, s.spool, s.im, s.mrmsMonitor)
	if m == nil {
		t.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		ServiceInstance:   s.mysqlInstance,
		MaxSlowLogSize:    1000,
		RemoveOldSlowLogs: true, // done after w2 and w1 done
		MaxWorkers:        2,    // w1 and w2 but not w3
		Interval:          60,
		WorkerRunTime:     60,
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

	// Start first mock worker (w1) with interval 0 - 736.  The worker's Run()
	// func won't return until we send true to its stop chan, so manager will
	// think worker is still running until then.
	now := time.Now()
	i1 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 0,
		EndOffset:   736,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i1
	<-w1.Running() // wait for manager to run worker

	// Start 2nd mock worker (w2) with interval 736 - 1833.  Manager will rotate
	// but not remove original slow log because w1 is still running.
	i2 := &qan.Interval{
		Filename:    "/tmp/" + slowlog,
		StartOffset: 736,
		EndOffset:   1833,
		StartTime:   now,
		StopTime:    now,
	}
	s.intervalChan <- i2
	<-w2.Running()

	test.WaitStatus(1, m, "qan-parser", "Idle (2 of 2 running)")

	/**
	 * Worker status test
	 */

	// Workers should have status and QAN manager should report them all.
	status := m.Status()
	t.Check(status["qan-worker-1"], Equals, "ok")
	t.Check(status["qan-worker-2"], Equals, "ok")
	t.Check(status["qan-worker-3"], Equals, "") // not running due to MaxWorkers

	/**
	 * Quick side test: qan.Config.MaxWorkers is enforced.
	 */
	test.DrainLogChan(s.logChan)
	s.intervalChan <- i2
	logs := test.WaitLogChan(s.logChan, 3)
	test.WaitStatus(1, m, "qan-parser", "Idle (2 of 2 running)")
	gotWarning := false
	for _, log := range logs {
		if log.Level == proto.LOG_WARNING && strings.Contains(log.Msg, "All workers busy") {
			gotWarning = true
			break
		}
	}
	if !gotWarning {
		t.Error("Too many workers causes \"All workers busy\" warning")
	}

	// Original slow log should no longer exist; it was rotated away, but...
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		t.Error("/tmp/" + slowlog + " no longer exists")
	}

	// ...old slow log should exist because w1 is still running.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 1 {
		t.Errorf("w1 running so old slow log not removed, got %+v", files)
	}
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	// Stop w2 which is holding "holding" the "lock" on removing the old
	// slog log (figuratively speaking; there are no real locks).  Because
	// w1 is still running, manager should not remove the old log yet because
	// w1 could still be parsing it.
	w2StopChan <- true
	test.WaitStatus(1, m, "qan-parser", "Idle (1 of 2 running)")
	if _, err := os.Stat(files[0]); os.IsNotExist(err) {
		t.Errorf("w1 still running so old slow log not removed")
	}

	// Stop w1 and now, even though slow log was rotated for w2, manager
	// should remove old slow log.
	w1StopChan <- true
	test.WaitStatus(1, m, "qan-parser", "Idle (0 of 2 running)")
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Errorf("w1 done running so old slow log removed")
	}

	// Stop manager
	reply = m.Handle(&proto.Cmd{Cmd: "StopService"})
	t.Assert(reply.Error, Equals, "")
}

func (s *ManagerTestSuite) TestRecoverWorkerPanic(t *C) {
	// Create and start manager with mock workers.
	w1StopChan := make(chan bool)
	w1 := mock.NewQanWorker("qan-worker-1", w1StopChan, nil, nil, true)
	f := mock.NewQanWorkerFactory([]*mock.QanWorker{w1})
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, f, s.spool, s.im, s.mrmsMonitor)
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
}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	t.Assert(m, NotNil)

	config := &qan.Config{
		ServiceInstance: s.mysqlInstance,
		Interval:        300,
		MaxSlowLogSize:  1000,
		MaxWorkers:      3,
		WorkerRunTime:   300,
		Start: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=0.456"},
			mysql.Query{Set: "SET GLOBAL slow_query_log=ON"},
		},
		Stop: []mysql.Query{
			mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set: "SET GLOBAL long_query_time=10"},
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

	s.nullmysql.Reset()

	cmd = &proto.Cmd{
		Cmd:     "GetConfig",
		Service: "qan",
	}
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfig := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfig); err != nil {
		t.Fatal(err)
	}
	expectConfig := []proto.AgentConfig{
		{
			InternalService: "qan",
			Config:          string(qanConfig),
			Running:         true,
		},
	}
	if same, diff := test.IsDeeply(gotConfig, expectConfig); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}

	// Stop manager
	reply = m.Handle(&proto.Cmd{Cmd: "StopService"})
	t.Assert(reply.Error, Equals, "")
}

func (s *ManagerTestSuite) TestStart(t *C) {
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, mockConnFactory, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	t.Assert(m, NotNil)

	// Starting qan without a config does nothing but start the qan manager.
	err := m.Start()
	t.Check(err, IsNil)

	status := m.Status()
	t.Check(status["qan-parser"], Equals, "")
	t.Check(status["qan-last-interval"], Equals, "")
	t.Check(status["qan-next-interval"], Equals, "")

	// Write a qan config to disk.
	config := &qan.Config{
		ServiceInstance: s.mysqlInstance,
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
		CollectFrom: "slowlog",
	}
	err = pct.Basedir.WriteConfig("qan", config)
	t.Assert(err, IsNil)

	// qan.Start() should read and use config on disk.
	err = m.Start()
	t.Check(err, IsNil)

	if !test.WaitStatusPrefix(1, m, "qan-parser", "Idle") {
		t.Error("WaitStatusPrefix(qan-parser, Idle) failed")
	}

	status = m.Status()
	t.Check(status["qan-parser"], Equals, "Idle (0 of 1 running)")
	t.Check(status["qan-last-interval"], Equals, "")
	t.Check(status["qan-next-interval"], Not(Equals), "")

	// Test GetGlobalDataFile. It must return an absolute path
	filename := m.GetGlobalDataFile("slow_query_log_file")
	if !path.IsAbs(filename) {
		t.Fail()
	}

	// Stopping qan.Stop() should leave config file on disk.
	err = m.Stop()
	t.Assert(err, IsNil)
	t.Check(test.FileExists(pct.Basedir.ConfigFile("qan")), Equals, true)
}

func (s *ManagerTestSuite) TestStartPfs(t *C) {
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	mysqlConn := mysql.NewConnection(s.dsn)
	err := mysqlConn.Connect(1)
	t.Assert(err, IsNil)
	defer mysqlConn.Close()

	// These queries eanble/configure perfomance_schema:
	start := []mysql.Query{
		mysql.Query{Verify: "performance_schema", Expect: "1"},
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'"},
		mysql.Query{Set: "TRUNCATE performance_schema.events_statements_summary_by_digest"},
	}

	// These queries disable performance_schema:
	stop := []mysql.Query{
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'NO' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'NO', TIMED = 'NO' WHERE NAME LIKE 'statement/sql/%'"},
	}

	// Disable perf schema because the qan manager should enable it when we start the pfs parser.
	if err := mysqlConn.Set(stop); err != nil {
		t.Fatal(err)
	}

	// Make a qan manager.
	m := qan.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.clock, s.iterFactory, s.workerFactory, s.spool, s.im, s.mrmsMonitor)
	t.Assert(m, NotNil)

	// Create the qan config for perf schema.
	config := &qan.Config{
		CollectFrom:     "perfschema", // <-- the magic
		ServiceInstance: s.mysqlInstance,
		Interval:        60, // 1m
		ExampleQueries:  true,
		MaxWorkers:      1,
		WorkerRunTime:   50, // 50s
		Start:           start,
		Stop:            stop,
	}

	// Create the StartService cmd which contains the qan config.
	now := time.Now()
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		User:      "Oleg",
		Ts:        now,
		AgentUuid: "123",
		Service:   "agent",
		Cmd:       "StartService",
		Data:      qanConfig,
	}

	// Have the qan manager start the pfs parser.
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	test.WaitStatus(1, m, "qan-parser", "Starting")
	tickChan := s.iterFactory.TickChans[s.iter]
	t.Assert(tickChan, NotNil)

	// Exec any query so there's at least 1 row/class in the pfs table.
	db := mysqlConn.DB()
	_, err = db.Exec("SELECT 1")

	// Send a fake interval to make qan manager run a pfs parser/worker.
	stopTs := time.Now()
	startTs := stopTs.Add(-1 * time.Minute)
	interv := &qan.Interval{
		StartTime: startTs,
		StopTime:  stopTs,
	}
	s.intervalChan <- interv

	// The pfs parser/worker parser the pfs interval and sends the result.
	v := test.WaitData(s.dataChan)
	t.Assert(v, HasLen, 1)
	report := v[0].(*qan.Report)
	t.Check(report.StartTs, Equals, startTs)
	t.Check(report.EndTs, Equals, stopTs)
	if len(report.Class) == 0 {
		t.Error("Report has no classes")
	}

	// Stop the pfs parser.
	cmd.Cmd = "StopService"
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
}

/////////////////////////////////////////////////////////////////////////////
// IntervalIter test suite
/////////////////////////////////////////////////////////////////////////////

type IntervalTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&IntervalTestSuite{})

func (s *IntervalTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
}

var fileName string

func getFilename() (string, error) {
	return fileName, nil
}

func (s *IntervalTestSuite) TestIterFile(t *C) {
	tickChan := make(chan time.Time)

	// This is the file we iterate.  It's 3 bytes large to start,
	// so that should be the StartOffset.
	tmpFile, _ := ioutil.TempFile("/tmp", "interval_test.")
	tmpFile.Close()
	fileName = tmpFile.Name()
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123"), 0777)
	defer func() { os.Remove(tmpFile.Name()) }()

	// Start interating the file, waiting for ticks.
	i := qan.NewFileIntervalIter(s.logger, getFilename, tickChan)
	i.Start()

	// Send a tick to start the interval
	t1 := time.Now()
	tickChan <- t1

	// Write more data to the file, pretend time passes...
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123456"), 0777)

	// Send a 2nd tick to finish the interval
	t2 := time.Now()
	tickChan <- t2

	// Get the interval
	got := <-i.IntervalChan()
	expect := &qan.Interval{
		Number:      1,
		Filename:    fileName,
		StartTime:   t1,
		StopTime:    t2,
		StartOffset: 3,
		EndOffset:   6,
	}
	t.Check(got, test.DeepEquals, expect)

	/**
	 * Rename the file, then re-create it.  The file change should be detected.
	 */

	oldFileName := tmpFile.Name() + "-old"
	os.Rename(tmpFile.Name(), oldFileName)
	defer os.Remove(oldFileName)

	// Re-create original file and write new data.  We expect StartOffset=0
	// because the file is new, and EndOffset=10 because that's the len of
	// the new data.  The old ^ file/data had start/stop offset 3/6, so those
	// should not appear in next interval; if they do, then iter failed to
	// detect file change and is still reading old file.
	tmpFile, _ = os.Create(fileName)
	tmpFile.Close()
	_ = ioutil.WriteFile(fileName, []byte("123456789A"), 0777)

	t3 := time.Now()
	tickChan <- t3

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Number:      2,
		Filename:    fileName,
		StartTime:   t2,
		StopTime:    t3,
		StartOffset: 0,
		EndOffset:   10,
	}
	t.Check(got, test.DeepEquals, expect)

	// Iter should no longer detect file change.
	_ = ioutil.WriteFile(fileName, []byte("123456789ABCDEF"), 0777)
	//                                               ^^^^^ new data
	t4 := time.Now()
	tickChan <- t4

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Number:      3,
		Filename:    fileName,
		StartTime:   t3,
		StopTime:    t4,
		StartOffset: 10,
		EndOffset:   15,
	}
	t.Check(got, test.DeepEquals, expect)

	i.Stop()
}

/////////////////////////////////////////////////////////////////////////////
// MakeReport (Result -> Report)
/////////////////////////////////////////////////////////////////////////////

type ReportTestSuite struct{}

var _ = Suite(&ReportTestSuite{})

func (s *ReportTestSuite) TestResult001(t *C) {
	data, err := ioutil.ReadFile(sample + "/result001.json")
	t.Assert(err, IsNil)

	result := &qan.Result{}
	err = json.Unmarshal(data, result)
	t.Assert(err, IsNil)

	start := time.Now().Add(-1 * time.Second)
	stop := time.Now()

	interval := &qan.Interval{
		Filename:    "slow.log",
		StartTime:   start,
		StopTime:    stop,
		StartOffset: 0,
		EndOffset:   1000,
	}
	config := qan.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		ReportLimit:     10,
	}
	report := qan.MakeReport(config, interval, result)

	// 1st: 2.9
	t.Check(report.Class[0].Id, Equals, "3000000000000003")
	t.Check(report.Class[0].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2.9))
	// 2nd: 2
	t.Check(report.Class[1].Id, Equals, "2000000000000002")
	t.Check(report.Class[1].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2))
	// ...
	// 5th: 0.101001
	t.Check(report.Class[4].Id, Equals, "5000000000000005")
	t.Check(report.Class[4].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(0.101001))

	// Limit=2 results in top 2 queries and the rest in 1 LRQ "query".
	config.ReportLimit = 2
	report = qan.MakeReport(config, interval, result)
	t.Check(len(report.Class), Equals, 3)

	t.Check(report.Class[0].Id, Equals, "3000000000000003")
	t.Check(report.Class[0].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2.9))

	t.Check(report.Class[1].Id, Equals, "2000000000000002")
	t.Check(report.Class[1].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2))

	t.Check(int(report.Class[2].TotalQueries), Equals, 3)
	t.Check(report.Class[2].Id, Equals, "0")
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(1+1+0.101001))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Min, Equals, float64(0.000100))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Max, Equals, float64(1.12))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Avg, Equals, float64(0.505))
}

func (s *SlowLogWorkerTestSuite) TestResult014(t *C) {
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow014.log",
		StartOffset:    0,
		EndOffset:      127118681,
		RunTime:        time.Duration(3 * time.Second),
		ZeroRunTime:    true,
		ExampleQueries: true,
	}
	w := qan.NewSlowLogWorker(s.logger, "qan-worker-1")
	result, _ := w.Run(job)

	start := time.Now().Add(-1 * time.Second)
	stop := time.Now()
	interval := &qan.Interval{
		Filename:    "slow.log",
		StartTime:   start,
		StopTime:    stop,
		StartOffset: 0,
		EndOffset:   127118680,
	}
	config := qan.Config{
		ServiceInstance: proto.ServiceInstance{Service: "mysql", InstanceId: 1},
		ReportLimit:     500,
	}
	report := qan.MakeReport(config, interval, result)

	t.Check(report.Global.TotalQueries, Equals, uint64(4))
	t.Check(report.Global.UniqueQueries, Equals, uint64(4))
	t.Assert(report.Class, HasLen, 4)
	// This query required improving the log parser to get the correct checksum ID:
	t.Check(report.Class[0].Id, Equals, "DB9EF18846547B8C")
}

/////////////////////////////////////////////////////////////////////////////
// PfsWorker test suite
/////////////////////////////////////////////////////////////////////////////

type PfsWorkerTestSuite struct {
	dsn     string
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&PfsWorkerTestSuite{})

func (s *PfsWorkerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
}

func (s *PfsWorkerTestSuite) TestCollectData(t *C) {
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	mysqlConn := mysql.NewConnection(s.dsn)
	err := mysqlConn.Connect(1)
	t.Assert(err, IsNil)
	defer mysqlConn.Close()

	enablePfs := []mysql.Query{
		mysql.Query{Verify: "performance_schema", Expect: "1"},
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'"},
		mysql.Query{Set: "TRUNCATE performance_schema.events_statements_summary_by_digest"},
	}
	if err := mysqlConn.Set(enablePfs); err != nil {
		t.Fatal(err)
	}

	db := mysqlConn.DB()
	_, err = db.Exec("SELECT NOW()")
	_, err = db.Exec("SELECT 1")
	_, err = db.Exec("SELECT * FROM `events_statements_summary_by_digest`")

	/**
	 * as we don't have consistent order in maps from Go v1.3,
	 * let's use pre-defined map with queries
	 */
	expectedResult := make(map[string]bool)
	expectedResult["TRUNCATE `performance_schema` . `events_statements_summary_by_digest` "] = true
	expectedResult["SELECT NOW ( ) "] = true
	expectedResult["SELECT ? "] = true
	expectedResult["SELECT * FROM `events_statements_summary_by_digest` "] = true

	w := qan.NewPfsWorker(s.logger, "pfs-worker", mysqlConn)
	gotPfsData, err := w.CollectData()
	t.Assert(err, IsNil)
	t.Assert(gotPfsData, NotNil)
	for i := range gotPfsData {
		if !expectedResult[gotPfsData[i].DigestText] {
			t.Errorf("Missing %s", gotPfsData[i].DigestText)
			test.Dump(gotPfsData)
		}
	}
}

func (s *PfsWorkerTestSuite) TestPrepareResult001(t *C) {
	parsedTime, _ := time.Parse("2006-01-02T15:04:05Z", "2014-07-10T19:14:30Z")
	pfsData := []*qan.PfsRow{
		{
			Digest:                  "d082a30b349166452cd1148310124d77",
			DigestText:              "TRUNCATE `events_statements_summary_by_digest` ",
			SumTimerWait:            588631000,
			MinTimerWait:            588631000,
			AvgTimerWait:            588631000,
			MaxTimerWait:            588631000,
			SumLockTime:             119000000,
			SumRowsAffected:         0,
			SumRowsSent:             0,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "973f7f10f95fc62e80148f2845ceca42",
			DigestText:              "SELECT NOW ( ) ",
			SumTimerWait:            41687000,
			MinTimerWait:            41687000,
			AvgTimerWait:            41687000,
			MaxTimerWait:            41687000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             1,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "93eaedb019bdfcf59f7aea8a25486ef0",
			DigestText:              "SELECT ? ",
			SumTimerWait:            20274000,
			MinTimerWait:            20274000,
			AvgTimerWait:            20274000,
			MaxTimerWait:            20274000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             1,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "fcc2b877639138358ef059551099f0d0",
			DigestText:              "SELECT * FROM `events_statements_summary_by_digest` ",
			SumTimerWait:            155949000,
			MinTimerWait:            155949000,
			AvgTimerWait:            155949000,
			MaxTimerWait:            155949000,
			SumLockTime:             38000000,
			SumRowsAffected:         0,
			SumRowsSent:             3,
			SumRowsExamined:         3,
			SumSelectFullJoin:       0,
			SumSelectScan:           1,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "2ea7017783cf24845827fd4e2cff1a5b",
			DigestText:              "USE `performance_schema` ",
			SumTimerWait:            24017000,
			MinTimerWait:            24017000,
			AvgTimerWait:            24017000,
			MaxTimerWait:            24017000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             0,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
	}

	w := qan.NewPfsWorker(s.logger, "pfs-worker", mock.NewNullMySQL())
	got, err := w.PrepareResult(pfsData)
	t.Assert(err, IsNil)
	t.Assert(got, NotNil)
	expect := &qan.Result{}
	err = test.LoadMmReport(sample+"pfs001.json", expect)
	t.Assert(err, IsNil)
	if ok, diff := test.IsDeeply(got, expect); !ok {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ValidateConfig test suite
/////////////////////////////////////////////////////////////////////////////

type ValidateConfigTestSuite struct {
}

var _ = Suite(&ValidateConfigTestSuite{})

func (s *ValidateConfigTestSuite) TestValidateConfig(t *C) {
	config := &qan.Config{
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
	err := qan.ValidateConfig(config)
	t.Check(err, IsNil)

	config = &qan.Config{
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
	err = qan.ValidateConfig(config)
	t.Check(err, NotNil)
}
