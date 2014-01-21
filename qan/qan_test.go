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
	"github.com/percona/percona-go-mysql/test"
	"io/ioutil"
	"launchpad.net/gocheck"
	"os"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

func RunWorker(job *qan.Job) string {
	logChan := make(chan *proto.LogEntry, 1000)
	logger := pct.NewLogger(logChan, "qan-test")
	dataChan := make(chan interface{}, 1)
	doneChan := make(chan *qan.Worker, 1)

	w := qan.NewWorker(logger, job, dataChan, doneChan)
	go w.Run()

	result := <-dataChan
	_ = <-doneChan

	// Write the result as formatted JSON to a file...
	tmpFilename := fmt.Sprintf("/tmp/pct-test.%d", os.Getpid())
	test.WriteData(result, tmpFilename)
	return tmpFilename
}

type WorkerTestSuite struct{}

var _ = gocheck.Suite(&WorkerTestSuite{})

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/qan/"

func (s *WorkerTestSuite) TestWorkerSlow001(c *gocheck.C) {
	job := &qan.Job{
		SlowLogFile:    testlog.Sample + "slow001.log",
		StartOffset:    0,
		StopOffset:     524,
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
		StopOffset:     358,
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
		StopOffset:     524,
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
	dsn string
	mysql *mysql.Connection
	reset []mysql.Query
}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(c *gocheck.C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		c.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	s.mysql = mysql.NewConnection(s.dsn)
	if err := s.mysql.Connect(); err != nil {
		c.Fatal(err)
	}
	if err := s.mysql.Connect(); err != nil {
		c.Fatal(err)
	}
	s.reset = []mysql.Query{
		mysql.Query{Set:"SET GLOBAL slow_query_log=OFF"},
		mysql.Query{Set:"SET GLOBAL long_query_time=10"},
	}
}

func (s *ManagerTestSuite) SetUpTest(c *gocheck.C) {
	err := s.mysql.Set(s.reset)
	if err != nil {
		c.Fatal(err)
	}
}

func (s *ManagerTestSuite) TearDownTest(c *gocheck.C) {
	err := s.mysql.Set(s.reset)
	if err != nil {
		c.Fatal(err)
	}
}

func (s *ManagerTestSuite) TestStartService(c *gocheck.C) {
	logChan := make(chan *proto.LogEntry, 1000)
	logger := pct.NewLogger(logChan, "qan-test")

	intervalChan := make(chan *qan.Interval, 1)
	iter := mock.NewMockIntervalIter(intervalChan)
	dataChan := make(chan interface{}, 2)

	m := qan.NewManager(logger, iter, dataChan)

	////////////////////////////////////////////////////////////////////////////
	// Start
	////////////////////////////////////////////////////////////////////////////

	// Create the qan config.
	tmpFile := fmt.Sprintf("/tmp/qan_test.TestStartService.%d", os.Getpid())
	defer func() { os.Remove(tmpFile) }()
	config := &qan.Config{
		DSN:               s.dsn,
		Start:             []mysql.Query{
			mysql.Query{Set:"SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set:"SET GLOBAL long_query_time=0.123"},
			mysql.Query{Set:"SET GLOBAL slow_query_log=ON"},
		},
		Stop:              []mysql.Query{
			mysql.Query{Set:"SET GLOBAL slow_query_log=OFF"},
			mysql.Query{Set:"SET GLOBAL long_query_time=10"},
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

	// It should have enabled the slow log.
	slowLog := s.mysql.GetGlobalVarNumber("slow_query_log")
	c.Assert(slowLog, gocheck.Equals, float64(1))

	longQueryTime := s.mysql.GetGlobalVarNumber("long_query_time")
	c.Assert(longQueryTime, gocheck.Equals, 0.123)

	////////////////////////////////////////////////////////////////////////////
	// Status
	////////////////////////////////////////////////////////////////////////////
	//time.Sleep(100 * time.Millisecond)
	//status := m.Status()
	//fmt.Println(status)

	////////////////////////////////////////////////////////////////////////////
	// runWorkers and sendData
	////////////////////////////////////////////////////////////////////////////
	// Send an Interval which should trigger a worker, and that worker will
	// send to the result which the sendData process will receive and then
	// send it via the dataChan.
	interv := &qan.Interval{
		Filename:    testlog.Sample + "slow001.log",
		StartOffset: 0,
		StopOffset:  524,
		StartTime:   now,
		StopTime:    now,
	}
	intervalChan <- interv

	resultData := <-dataChan
	c.Assert(resultData, gocheck.NotNil)
	test.WriteData(resultData, tmpFile)
	c.Check(tmpFile, testlog.FileEquals, sample+"slow001.json")

	////////////////////////////////////////////////////////////////////////////
	// Stop
	////////////////////////////////////////////////////////////////////////////

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
	slowLog = s.mysql.GetGlobalVarNumber("slow_query_log")
	c.Assert(slowLog, gocheck.Equals, float64(0))

	longQueryTime = s.mysql.GetGlobalVarNumber("long_query_time")
	c.Assert(longQueryTime, gocheck.Equals, 10.0)
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
		StopOffset:  6,
	}
	c.Check(got, test.DeepEquals, expect)

	/**
	 * Rename the file, then re-create it.  The file change should be detected.
	 */

	oldFileName := tmpFile.Name() + "-old"
	os.Rename(tmpFile.Name(), oldFileName)
	defer os.Remove(oldFileName)

	// Re-create original file and write new data.  We expect StartOffset=0
	// because the file is new, and StopOffset=10 because that's the len of
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
		StopOffset:  10,
	}
	c.Check(got, test.DeepEquals, expect)

	// Iter should no longer detect file change.
	_ = ioutil.WriteFile(fileName, []byte("123456789ABCDEF"), 0777)
	// ^^^^^ new data
	t4 := time.Now()
	tickerChan <- t4

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Filename:    fileName,
		StartTime:   t3,
		StopTime:    t4,
		StartOffset: 10,
		StopOffset:  15,
	}
	c.Check(got, test.DeepEquals, expect)

	i.Stop()
}
