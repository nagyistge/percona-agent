package qh_test

import (
	"os"
	"time"
	"launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-go-mysql/test"
	"github.com/percona/percona-cloud-tools/test"
	"github.com/percona/percona-cloud-tools/qh"
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
// Interval test suite
/////////////////////////////////////////////////////////////////////////////

type IntervalTestSuite struct{}
var _ = gocheck.Suite(&IntervalTestSuite{})

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/percona-cloud-tools/test/qh/"

func (s *IntervalTestSuite) TestInterval(c *gocheck.C) {
}

