package qan_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	mysqlLog "github.com/percona/percona-go-mysql/log"
	"github.com/percona/percona-go-mysql/test"
	"io/ioutil"
	"launchpad.net/gocheck"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/qan/"

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

type WorkerTestSuite struct{}

var _ = gocheck.Suite(&WorkerTestSuite{})

func RunWorker(job *qan.Job) string {
	w := qan.NewSlowLogWorker()
	result, _ := w.Run(job)

	// Write the result as formatted JSON to a file...
	tmpFilename := fmt.Sprintf("/tmp/pct-test.%d", os.Getpid())
	test.WriteData(result, tmpFilename)
	return tmpFilename
}

func (s *WorkerTestSuite) TestWorkerSlow001(c *gocheck.C) {
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		EndOffset:      524,
		RunTime:        time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := RunWorker(job)
	defer os.Remove(tmpFilename)

	// ...then diff <result file> <expected result file>
	// @todo need a generic testlog.DeeplEquals
	c.Assert(tmpFilename, testlog.FileEquals, sample+"slow001.json")
}

func (s *WorkerTestSuite) TestWorkerSlow001Half(c *gocheck.C) {
	// This tests that the worker will stop processing events before
	// the end of the slow log file.  358 is the last byte of the first
	// (of 2) events.
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		EndOffset:      358,
		RunTime:        time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := RunWorker(job)
	defer os.Remove(tmpFilename)
	c.Assert(tmpFilename, testlog.FileEquals, sample+"slow001-half.json")
}

func (s *WorkerTestSuite) TestWorkerSlow001Resume(c *gocheck.C) {
	// This tests that the worker will resume processing events from
	// somewhere in the slow log file.  359 is the first byte of the
	// second (of 2) events.
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    359,
		EndOffset:      524,
		RunTime:        time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := RunWorker(job)
	defer os.Remove(tmpFilename)
	c.Assert(tmpFilename, testlog.FileEquals, sample+"slow001-resume.json")
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	dsn           string
	realmysql     *mysql.Connection
	nullmysql     *mock.NullMySQL
	reset         []mysql.Query
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	intervalChan  chan *qan.Interval
	iter          qan.IntervalIter
	dataChan      chan interface{}
	spool         *mock.Spooler
	workerFactory qan.WorkerFactory
}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(c *gocheck.C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		c.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	s.realmysql = mysql.NewConnection()
	if err := s.realmysql.Connect(s.dsn); err != nil {
		c.Fatal(err)
	}
	s.reset = []mysql.Query{
		mysql.Query{Set: "SET GLOBAL slow_query_log=OFF"},
		mysql.Query{Set: "SET GLOBAL long_query_time=10"},
	}

	s.nullmysql = mock.NewNullMySQL()

	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	s.intervalChan = make(chan *qan.Interval, 1)
	s.iter = mock.NewMockIntervalIter(s.intervalChan)
	s.dataChan = make(chan interface{}, 2)

	s.spool = mock.NewSpooler(s.dataChan)
	s.workerFactory = qan.NewSlowLogWorkerFactory()
}

func (s *ManagerTestSuite) SetUpTest(c *gocheck.C) {
	err := s.realmysql.Set(s.reset)
	if err != nil {
		c.Fatal(err)
	}
	s.nullmysql.Reset()
}

func (s *ManagerTestSuite) TearDownTest(c *gocheck.C) {
	err := s.realmysql.Set(s.reset)
	if err != nil {
		c.Fatal(err)
	}
}

func (s *ManagerTestSuite) TestStartService(c *gocheck.C) {

	/**
	 * Create and start manager.
	 */

	m := qan.NewManager(s.logger, s.realmysql, s.iter, s.workerFactory, s.spool)
	if m == nil {
		c.Fatal("Create qan.Manager")
	}

	// Create the qan config.
	tmpFile := fmt.Sprintf("/tmp/qan_test.TestStartService.%d", os.Getpid())
	defer func() { os.Remove(tmpFile) }()
	config := &qan.Config{
		DSN: s.dsn,
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
		ExampleQueries:    false,
		MaxWorkers:        2,
		WorkerRunTime:     600, // 10 min
	}

	// Create the StartService cmd which contains the qan config.
	now := time.Now()
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUuid: "123",
		Service:   "",
		Cmd:       "StartService",
		Data:      qanConfig,
	}

	// Have the service manager start the qa service
	err := m.Start(cmd, cmd.Data)

	// It should start without error.
	c.Assert(err, gocheck.IsNil)

	// It should report itself as running
	if !m.IsRunning() {
		c.Error("Manager.IsRunning() is false after Start()")
	}

	// And status should be "Running" and "Ready".
	test.WaitStatus(1, m, "QanLogParser", "Ready (0 of 2 running)")
	status := m.Status()
	if status["Qan"] != "Running ["+cmd.String()+"]" {
		c.Error("Qan status is \"Ready\", got " + status["Qan"])
	}
	if status["QanLogParser"] != "Ready (0 of 2 running)" {
		c.Error("QanLogParser status is \"Ready (0 of 2 running)\", got " + status["QanLogParser"])
	}

	// It should have enabled the slow log.
	slowLog := s.realmysql.GetGlobalVarNumber("slow_query_log")
	c.Assert(slowLog, gocheck.Equals, float64(1))

	longQueryTime := s.realmysql.GetGlobalVarNumber("long_query_time")
	c.Assert(longQueryTime, gocheck.Equals, 0.123)

	// Starting an already started service should result in a ServiceIsRunningError.
	err = m.Start(cmd, cmd.Data)
	if err == nil {
		c.Error("Start manager when already start cauess error")
	}
	switch err.(type) { // todo: ugly hack to access and test error type
	case pct.ServiceIsRunningError:
		// ok
	default:
		c.Error("Error is type pct.ServiceIsRunningError, got %T", err)
	}

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

	resultData := <-s.dataChan
	if resultData == nil {
		c.Fatal("Got result from worker ran by manager")
	}

	report := *resultData.(*proto.QanReport)
	global := &mysqlLog.GlobalClass{}
	json.Unmarshal(report.Global, global)
	classes := []*mysqlLog.QueryClass{}
	err = json.Unmarshal(report.Class, &classes)
	if err != nil {
		c.Error(err)
	}
	result := &qan.Result{
		StopOffset: report.StopOffset,
		Global:     global,
		Classes:    classes,
	}
	test.WriteData(result, tmpFile)
	c.Check(tmpFile, testlog.FileEquals, sample+"slow001.json")

	/**
	 * Stop manager like API would: by sending StopService cmd.
	 */

	now = time.Now()
	cmd = &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUuid: "123",
		Service:   "",
		Cmd:       "StopService",
	}

	// Have the service manager start the qa service
	err = m.Stop(cmd)

	// It should start without error.
	c.Assert(err, gocheck.IsNil)

	// It should report itself as running
	if m.IsRunning() {
		c.Error("Manager.IsRunning() is false after Start()")
	}

	// It should disable the slow log.
	slowLog = s.realmysql.GetGlobalVarNumber("slow_query_log")
	c.Assert(slowLog, gocheck.Equals, float64(0))

	longQueryTime = s.realmysql.GetGlobalVarNumber("long_query_time")
	c.Assert(longQueryTime, gocheck.Equals, 10.0)
}

func (s *ManagerTestSuite) TestRotateAndRemoveSlowLog(c *gocheck.C) {

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
	m := qan.NewManager(s.logger, s.nullmysql, s.iter, s.workerFactory, s.spool)
	if m == nil {
		c.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		DSN:               s.dsn,
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
	}
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		Ts:   time.Now(),
		Cmd:  "StartService",
		Data: qanConfig,
	}
	err := m.Start(cmd, cmd.Data)
	if err != nil {
		c.Fatal(err)
	}
	test.WaitStatus(1, m, "QanLogParser", "Ready")

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
	result := *resultData.(*proto.QanReport)
	global := &mysqlLog.GlobalClass{}
	json.Unmarshal(result.Global, global)
	if global.TotalQueries != 2 {
		c.Error("First interval has 2 queries, got ", global.TotalQueries)
	}
	if global.UniqueQueries != 1 {
		c.Error("First interval has 1 unique query, got ", global.UniqueQueries)
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
	result = *resultData.(*proto.QanReport)
	global = &mysqlLog.GlobalClass{}
	json.Unmarshal(result.Global, global)
	if global.TotalQueries != 4 {
		c.Error("Second interval has 2 queries, got ", global.TotalQueries)
	}
	if global.UniqueQueries != 2 {
		c.Error("Second interval has 2 unique queries, got ", global.UniqueQueries)
	}

	test.WaitStatus(1, m, "QanLogParser", "Ready (0 of 2 running)")

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		c.Error("/tmp/" + slowlog + " no longer exists")
	}

	// The original slow log should have been renamed to slow006-TS, parsed, and removed.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 0 {
		c.Errorf("Old slow log removed, got %+v", files)
	}
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	// Stop manager
	err = m.Stop(&proto.Cmd{Cmd: "StopService"})
	c.Assert(err, gocheck.IsNil)
}

func (s *ManagerTestSuite) TestRotateSlowLog(c *gocheck.C) {

	// Same as TestRotateAndRemoveSlowLog, but with qan.Config.RemoveOldSlowLogs=false
	// and testing that Start and Stop queries were executed.

	slowlog := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}

	m := qan.NewManager(s.logger, s.nullmysql, s.iter, s.workerFactory, s.spool)
	if m == nil {
		c.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		DSN:               s.dsn,
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
	}
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		Ts:   time.Now(),
		Cmd:  "StartService",
		Data: qanConfig,
	}
	err := m.Start(cmd, cmd.Data)
	if err != nil {
		c.Fatal(err)
	}
	test.WaitStatus(1, m, "QanLogParser", "Ready")

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
	result := *resultData.(*proto.QanReport)
	global := &mysqlLog.GlobalClass{}
	json.Unmarshal(result.Global, global)
	if global.TotalQueries != 2 {
		c.Error("First interval has 2 queries, got ", global.TotalQueries)
	}
	if global.UniqueQueries != 1 {
		c.Error("First interval has 1 unique query, got ", global.UniqueQueries)
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
	result = *resultData.(*proto.QanReport)
	global = &mysqlLog.GlobalClass{}
	json.Unmarshal(result.Global, global)
	if global.TotalQueries != 4 {
		c.Error("Second interval has 2 queries, got ", global.TotalQueries)
	}
	if global.UniqueQueries != 2 {
		c.Error("Second interval has 2 unique queries, got ", global.UniqueQueries)
	}

	test.WaitStatus(1, m, "QanLogParser", "Ready (0 of 2 running)")

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		c.Error("/tmp/" + slowlog + " no longer exists")
	}

	// The original slow log should NOT have been removed.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 1 {
		c.Errorf("Old slow log not removed, got %+v", files)
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
		c.Logf("%+v", s.nullmysql.GetSet())
		c.Logf("%+v", expect)
		c.Error(diff)
	}

	// Stop manager
	err = m.Stop(&proto.Cmd{Cmd: "StopService"})
	c.Assert(err, gocheck.IsNil)

}

func (s *ManagerTestSuite) TestWaitRemoveSlowLog(c *gocheck.C) {

	// Same as TestRotateAndRemoveSlowLog, but we use mock workers so we can
	// test that slow log is not removed until previous workers are done.
	// Mock worker factory will return our mock workers when manager calls Make().
	w1StopChan := make(chan bool)
	w1 := mock.NewQanWorker(w1StopChan, nil, nil)

	w2StopChan := make(chan bool)
	w2 := mock.NewQanWorker(w2StopChan, nil, nil)

	// Let's take this time to also test that MaxWorkers is enforced.
	w3 := mock.NewQanWorker(nil, nil, nil)

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
	m := qan.NewManager(s.logger, s.nullmysql, s.iter, f, s.spool)
	if m == nil {
		c.Fatal("Create qan.Manager")
	}
	config := &qan.Config{
		// very abbreviated qan.Config because we're mocking a lot
		MaxSlowLogSize:    1000,
		RemoveOldSlowLogs: true, // done after w2 and w1 done
		MaxWorkers:        2,    // w1 and w2 but not w3
	}
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		Ts:   time.Now(),
		Cmd:  "StartService",
		Data: qanConfig,
	}
	err := m.Start(cmd, cmd.Data)
	if err != nil {
		c.Fatal(err)
	}
	test.WaitStatus(1, m, "QanLogParser", "Ready")

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

	test.WaitStatus(1, m, "QanLogParser", "Ready (2 of 2 running)")

	/**
	 * Quick side test: qan.Config.MaxWorkers is enforced.
	 */
	test.DrainLogChan(s.logChan)
	s.intervalChan <- i2
	logs := test.WaitLogChan(s.logChan, 3)
	test.WaitStatus(1, m, "QanLogParser", "Ready (2 of 2 running)")
	gotWarning := false
	for _, log := range logs {
		if log.Level == proto.LOG_WARNING && strings.Contains(log.Msg, "All workers busy") {
			gotWarning = true
			break
		}
	}
	if !gotWarning {
		c.Error("Too many workers causes \"All workers busy\" warning")
	}

	// Original slow log should no longer exist; it was rotated away, but...
	if _, err := os.Stat("/tmp/" + slowlog); !os.IsNotExist(err) {
		c.Error("/tmp/" + slowlog + " no longer exists")
	}

	// ...old slow log should exist because w1 is still running.
	files, _ = filepath.Glob("/tmp/" + slowlog + "-[0-9]*")
	if len(files) != 1 {
		c.Errorf("w1 running so old slow log not removed, got %+v", files)
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
	test.WaitStatus(1, m, "QanLogParser", "Ready (1 of 2 running)")
	if _, err := os.Stat(files[0]); os.IsNotExist(err) {
		c.Errorf("w1 still running so old slow log not removed")
	}

	// Stop w1 and now, even though slow log was rotated for w2, manager
	// should remove old slow log.
	w1StopChan <- true
	test.WaitStatus(1, m, "QanLogParser", "Ready (0 of 2 running)")
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		c.Errorf("w1 done running so old slow log removed")
	}

	// Stop manager
	err = m.Stop(&proto.Cmd{Cmd: "StopService"})
	c.Assert(err, gocheck.IsNil)

}

/////////////////////////////////////////////////////////////////////////////
// IntervalIter test suite
/////////////////////////////////////////////////////////////////////////////

type IntervalTestSuite struct{}

var _ = gocheck.Suite(&IntervalTestSuite{})

var fileName string

func getFilename() (string, error) {
	return fileName, nil
}

func (s *IntervalTestSuite) TestIterFile(c *gocheck.C) {
	tickerChan := make(chan time.Time)

	// This is the file we iterate.  It's 3 bytes large to start,
	// so that should be the StartOffset.
	tmpFile, _ := ioutil.TempFile("/tmp", "interval_test.")
	tmpFile.Close()
	fileName = tmpFile.Name()
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123"), 0777)
	defer func() { os.Remove(tmpFile.Name()) }()

	// Start interating the file, waiting for ticks.
	i := qan.NewFileIntervalIter(getFilename, tickerChan)
	i.Start()

	// Send a tick to start the interval
	t1 := time.Now()
	tickerChan <- t1

	// Write more data to the file, pretend time passes...
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123456"), 0777)

	// Send a 2nd tick to finish the interval
	t2 := time.Now()
	tickerChan <- t2

	// Get the interval
	got := <-i.IntervalChan()
	expect := &qan.Interval{
		Filename:    fileName,
		StartTime:   t1,
		StopTime:    t2,
		StartOffset: 3,
		EndOffset:   6,
	}
	c.Check(got, test.DeepEquals, expect)

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
	tickerChan <- t3

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Filename:    fileName,
		StartTime:   t2,
		StopTime:    t3,
		StartOffset: 0,
		EndOffset:   10,
	}
	c.Check(got, test.DeepEquals, expect)

	// Iter should no longer detect file change.
	_ = ioutil.WriteFile(fileName, []byte("123456789ABCDEF"), 0777)
	//                                               ^^^^^ new data
	t4 := time.Now()
	tickerChan <- t4

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Filename:    fileName,
		StartTime:   t3,
		StopTime:    t4,
		StartOffset: 10,
		EndOffset:   15,
	}
	c.Check(got, test.DeepEquals, expect)

	i.Stop()
}
