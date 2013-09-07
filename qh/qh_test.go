package qh

import (
//	"fmt"
	"time"
	"launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-go-mysql/test"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

type WorkerTestSuite struct{}
var _ = gocheck.Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) TestWorker(c *gocheck.C) {
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

	c.Assert(1, gocheck.Equals, 1)
}
