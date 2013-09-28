package interval_test

import (
//	"fmt"
	"time"
	"launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-cloud-tools/qh/interval"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Syncer test suite
/////////////////////////////////////////////////////////////////////////////

type SyncerTestSuite struct{}
var _ = gocheck.Suite(&SyncerTestSuite{})

// Fake time.Sleep()
var slept time.Duration
func sleep(t time.Duration) {
	slept = t
	return
}

func (s *SyncerTestSuite) TestSleepTime(c *gocheck.C) {
	/*
	 * To sync at intervals, we must first sleep N number of nanoseconds
	 * until the next interval.  So we specify the curren time (now) in
	 * nanaseconds, and an interval time (2), and then we know how long
	 * the syncer should sleep to wait from now until the next interval
	 * time.
	 */

	// Fri Sep 27 18:11:37.385120 -0700 PDT 2013 =
	now := int64(1380330697385120263)
	// The next 2s interval, 18:11:40.000, is 614879744 nanoseconds away,
	// so that's how long syncer should tell our sleep func to sleep.
	sync := interval.NewSyncer(2, sleep)
	_ = sync.Sync(now)
	c.Assert(slept.Nanoseconds(), gocheck.Equals, int64(614879744))
}

func (s *SyncerTestSuite) TestTickerTime(c *gocheck.C) {
	/*
	 * The ticker returned by the syncer should tick at this given interval,
	 * 2s in this case.  We test this by ensuring that the current time at
	 * a couple of ticks is divisible by 2, and that the fractional seconds
	 * part is < 100 microseconds (0.000100000 which is just 100000) because
	 * the ticks are only precise into the millisecond range--at least on
	 * my laptop--and that's good enough.  So ticks are like:
	 *   00:00:02.000123456
	 *   00:00:04.000123456
     */
	sync := interval.NewSyncer(2, time.Sleep)
	t := sync.Sync(time.Now().UnixNano())
	maxOffBy := 100000
	for i := 0; i < 2; i++ {
		select {
		case t := <-t.C:
			// 0.000 100 000
			sec := t.Second()
			if sec % 2 > 0 {
				c.Errorf("Tick %d not 2s interval: %d", i, sec)
			}
			nano := t.Nanosecond()
			if nano < maxOffBy {
				c.Errorf("Tick %d failed: %d >= %d", i, nano, maxOffBy)
			}
		}
	}
}
