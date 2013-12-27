package qan_test

import (
	"encoding/json"
	"fmt"
	proto "github.com/percona/cloud-protocol"
	pct "github.com/percona/cloud-tools"
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
	resultChan := make(chan *qan.Result, 1)
	doneChan := make(chan *qan.Worker, 1)

	w := qan.NewWorker(logger, job, resultChan, doneChan)
	go w.Run()

	result := <-resultChan
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
		Runtime:        time.Duration(3 * time.Second),
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
		Runtime:        time.Duration(3 * time.Second),
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
		Runtime:        time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := RunWorker(job)
	defer os.Remove(tmpFilename)
	c.Assert(tmpFilename, testlog.FileEquals, sample+"slow001-resume.json")
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct{}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) TestStartService(c *gocheck.C) {
	logChan := make(chan *proto.LogEntry, 1000)
	logger := pct.NewLogger(logChan, "qan-test")

	sendChan := make(chan *proto.Cmd, 5)
	recvChan := make(chan *proto.Reply, 5)
	client := mock.NewWebsocketClient(sendChan, recvChan, nil, nil)
	go client.Run()

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
		Interval:          300, // 5 min
		LongQueryTime:     0.000000,
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    false,
		MysqlDsn:          "test:test@tcp(127.0.0.1:3306)/",
		MaxWorkers:        2,
		WorkerRuntime:     600, // 10 min
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
}

/////////////////////////////////////////////////////////////////////////////
// IntervalIter test suite
/////////////////////////////////////////////////////////////////////////////

type IntervalIterTestSuite struct{}

var _ = gocheck.Suite(&IntervalIterTestSuite{})

var fileName string

func getFilename() (string, error) {
	return fileName, nil
}

func (s *IntervalIterTestSuite) TestIntervalIter(c *gocheck.C) {
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

	// Stop the interator and get the interval it sent us
	i.Stop()
	got := <-i.IntervalChan()
	expect := &qan.Interval{
		Filename:    fileName,
		StartTime:   t1,
		StopTime:    t2,
		StartOffset: 3,
		StopOffset:  6,
	}
	c.Check(got, gocheck.DeepEquals, expect)
}
