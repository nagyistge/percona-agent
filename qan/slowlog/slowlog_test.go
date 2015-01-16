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

package slowlog_test

import (
	"io/ioutil"
	"os"
	"sort"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/go-mysql/event"
	gomysql "github.com/percona/go-mysql/test"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/qan/slowlog"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var inputDir = gomysql.RootDir + "/test/slow-logs/"
var outputDir = RootDir() + "/test/qan/"

type ByQueryId []*event.QueryClass

func (a ByQueryId) Len() int      { return len(a) }
func (a ByQueryId) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByQueryId) Less(i, j int) bool {
	return a[i].Id > a[j].Id
}

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

type WorkerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	now           time.Time
	mysqlInstance proto.ServiceInstance
	config        qan.Config
	mysqlConn     mysql.Connector
	worker        *slowlog.Worker
}

var _ = Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
	s.now = time.Now()
	s.mysqlInstance = proto.ServiceInstance{Service: "mysql", InstanceId: 1}
	s.config = qan.Config{
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
		Interval:          60,         // 1 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		WorkerRunTime:     60, // 1 min
		CollectFrom:       "slowlog",
	}
}

func (s *WorkerTestSuite) RunWorker(config qan.Config, mysqlConn mysql.Connector, i *qan.Interval) (*qan.Result, error) {
	w := slowlog.NewWorker(s.logger, config, mysqlConn)
	w.ZeroRunTime = true
	w.Setup(i)
	err, res := w.Run()
	w.Cleanup()
	return err, res
}

func (s *WorkerTestSuite) TestWorkerSlow001(t *C) {
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadMmReport(outputDir+"slow001.json", expect)
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001NoExamples(t *C) {
	i := &qan.Interval{
		Number:      99,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
	}
	config := s.config
	config.ExampleQueries = false
	got, err := s.RunWorker(config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadMmReport(outputDir+"slow001-no-examples.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	// Worker should be able to report its name and status.
	//t.Check(w.Name(), Equals, "qan-worker-1")
	//t.Check(w.Status(), Equals, "Done job "+job.Id)
}

func (s *WorkerTestSuite) TestWorkerSlow001Half(t *C) {
	// This tests that the worker will stop processing events before
	// the end of the slow log file.  358 is the last byte of the first
	// (of 2) events.
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   358,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadMmReport(outputDir+"slow001-half.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001Resume(t *C) {
	// This tests that the worker will resume processing events from
	// somewhere in the slow log file.  359 is the first byte of the
	// second (of 2) events.
	i := &qan.Interval{
		Number:      2,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 359,
		EndOffset:   524,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadMmReport(outputDir+"slow001-resume.json", expect)
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow011(t *C) {
	// Percona Server rate limit
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow011.log",
		StartOffset: 0,
		EndOffset:   3000,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadMmReport(outputDir+"slow011.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

// Rotate and remove slow logs

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
	cp := exec.Command("cp", inputDir+slowlog, "/tmp/"+slowlog)
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
	cp := exec.Command("cp", inputDir+slowlog, "/tmp/"+slowlog)
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
	if same, diff := IsDeeply(s.nullmysql.GetSet(), expect); !same {
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
	cp := exec.Command("cp", inputDir+slowlog, "/tmp/"+slowlog)
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


/////////////////////////////////////////////////////////////////////////////
// IntervalIter test suite
/////////////////////////////////////////////////////////////////////////////

type IterTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&IterTestSuite{})

func (s *IterTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
}

var fileName string

func getFilename() (string, error) {
	return fileName, nil
}

func (s *IterTestSuite) TestIterFile(t *C) {
	tickChan := make(chan time.Time)

	// This is the file we iterate.  It's 3 bytes large to start,
	// so that should be the StartOffset.
	tmpFile, _ := ioutil.TempFile("/tmp", "interval_test.")
	tmpFile.Close()
	fileName = tmpFile.Name()
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123"), 0777)
	defer func() { os.Remove(tmpFile.Name()) }()

	// Start interating the file, waiting for ticks.
	i := slowlog.NewIter(s.logger, getFilename, tickChan)
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
