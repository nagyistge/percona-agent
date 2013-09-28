package qh_test

import (
	"os"
	"fmt"
	"time"
	"encoding/json"
	"launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-go-mysql/test"
	"github.com/percona/percona-cloud-tools/test"
	"github.com/percona/percona-cloud-tools/test/mock"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/qh"
	"github.com/percona/percona-cloud-tools/qh/interval"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

type WorkerTestSuite struct{}
var _ = gocheck.Suite(&WorkerTestSuite{})

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/percona-cloud-tools/test/qh/"

func (s *WorkerTestSuite) TestWorkerSlow001(c *gocheck.C) {
	job := &qh.Job{
		SlowLogFile: testlog.Sample + "slow001.log",
		StartOffset: 0,
		StopOffset: 524,
		Runtime: time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := test.RunQhWorker(job)
	defer os.Remove(tmpFilename)

	// ...then diff <result file> <expected result file>
	// @todo need a generic testlog.DeeplEquals
	c.Assert(tmpFilename, testlog.FileEquals, sample + "slow001.json")
}

func (s *WorkerTestSuite) TestWorkerSlow001Half(c *gocheck.C) {
	// This tests that the worker will stop processing events before
	// the end of the slow log file.  358 is the last byte of the first
	// (of 2) events.
	job := &qh.Job{
		SlowLogFile: testlog.Sample + "slow001.log",
		StartOffset: 0,
		StopOffset: 358,
		Runtime: time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := test.RunQhWorker(job)
	defer os.Remove(tmpFilename)
	c.Assert(tmpFilename, testlog.FileEquals, sample + "slow001-half.json")
}

func (s *WorkerTestSuite) TestWorkerSlow001Resume(c *gocheck.C) {
	// This tests that the worker will resume processing events from
	// somewhere in the slow log file.  359 is the first byte of the
	// second (of 2) events.
	job := &qh.Job{
		SlowLogFile: testlog.Sample + "slow001.log",
		StartOffset: 359,
		StopOffset: 524,
		Runtime: time.Duration(3 * time.Second),
		ExampleQueries: true,
	}
	tmpFilename := test.RunQhWorker(job)
	defer os.Remove(tmpFilename)
	c.Assert(tmpFilename, testlog.FileEquals, sample + "slow001-resume.json")
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct{}
var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) TestStartService(c *gocheck.C) {
	// Create the service manager
	cc := &agent.ControlChannels{
		LogChan: make(chan *log.LogEntry, 10),
		StopChan: make(chan bool, 1),
		DoneChan: make(chan bool),
	}
	mockIter := &mock.Iter{
		Chan: make(chan *interval.Interval, 1),
	}
	resultChan := make(chan *qh.Result, 2)
	sentDataChan := make(chan interface{}, 2)
	dataClient := &mock.NullClient{
		SentDataChan: sentDataChan,
	}
	m := qh.NewManager(cc, mockIter, resultChan, dataClient)

	// Create the qh config
	dataDir := fmt.Sprintf("/tmp/qh_test.TestStartService.%d", os.Getpid())
	defer func() { os.Remove(dataDir) }()
	config := &qh.Config{
		Interval: 300,  // 5 min
		LongQueryTime: 0.000000,
		MaxSlowLogSize: 1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries: false,
		MysqlDsn: "test:test@tcp(127.0.0.1:3306)/",
		MaxWorkers: 2,
		WorkerRuntime: 600, // 10 min
		DataDir: dataDir,
	}

	// Create the StartService cmd which contains the qh config
	now := time.Now()
	data, _ := json.Marshal(config)
	msg := &proto.Msg{
		User: "daniel",
		Id: 1,
		Cmd: "StartService",
		Ts: now,
		Timeout: 1,
		Data: data, // qh.Config
	}

	// Have the service manager start the qh service
	err := m.Start(msg, msg.Data)

	// It should start without error.
	c.Assert(err, gocheck.IsNil)

	// It should report itself as running
	if !m.IsRunning() {
		c.Error("qh.Manager.IsRunning() is false after Start()")
	}

	// It should create the qh.Config.DataDir.
	stat, err := os.Stat(dataDir)
	if err != nil {
		c.Errorf("qh.Manager.Start() did not create qh.Config.DataDir: %s", dataDir)
	}
	if !stat.IsDir() {
		c.Errorf("qh.Config.DataDir is not a dir: %s", dataDir)
	}

	// @todo
	//time.Sleep(100 * time.Millisecond)
	//status := m.Status()
	//fmt.Println(status)

	// Send an Interval which should trigger a worker, and that worker will
	// send to the result which the sendData process will receive and then
	// send it via the dataClient.
	interv := &interval.Interval{
		Filename: testlog.Sample + "slow001.log",
		StartOffset: 0,
		StopOffset: 524,
		StartTime: now,
		StopTime: now,
	}

	mockIter.Chan <-interv
	resultData := <-sentDataChan
	resultFile := dataDir + "/result.json"
	test.WriteData(resultData, resultFile)
	c.Check(resultFile, testlog.FileEquals, sample + "slow001.json")
}
