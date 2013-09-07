package qh

import (
	"os"
	"fmt"
	"time"
	"io/ioutil"
	"launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-go-mysql/test"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"encoding/json"
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
	cc := &agent.ControlChannels{
		LogChan: make(chan *log.LogEntry),
		StopChan: make(chan bool),
	}
	resultChan := make(chan *Result, 1)
	doneChan := make(chan *Worker, 1)

	job := &Job{
		SlowLogFile: testlog.Sample + "slow001.log",
		StartOffset: 0,
		StopOffset: 524,
		Runtime: time.Duration(3 * time.Second),
		ExampleQueries: true,
		cc: cc,
		ResultChan: resultChan,
		DoneChan: doneChan,
	}
	w := NewWorker(job)
	w.Run()

	// Write the result as formatted JSON to a file...
	result := <-resultChan
	bytes, _ := json.MarshalIndent(result, "", " ")
	bytes = append(bytes, 0x0A) // newline
	tmpFilename := fmt.Sprintf("/tmp/pct-test.%d", os.Getpid())
	ioutil.WriteFile(tmpFilename, bytes, os.ModePerm)
	defer os.Remove(tmpFilename)

	// ...then diff <result file> <expected result file>
	// @todo need a generical testlog.DeeplEquals
	c.Assert(tmpFilename, testlog.FileEquals, sample + "slow001.json")
}
