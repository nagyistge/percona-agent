package qh

import (
	"launchpad.net/gocheck"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

type WorkerTestSuite struct{}
var _ = gocheck.Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) TestWorker(c *gocheck.C) {
	c.Assert(1, gocheck.Equals, 1)
}
