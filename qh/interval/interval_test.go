package interval_test

import (
//	"fmt"
	"os"
	"io/ioutil"
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

/////////////////////////////////////////////////////////////////////////////
// Iter test suite
/////////////////////////////////////////////////////////////////////////////

type IterTestSuite struct{}
var _ = gocheck.Suite(&IterTestSuite{})

var fileName string
func getFilename() (string, error) {
	return fileName, nil
}

func (s *IterTestSuite) TestIter(c *gocheck.C) {
	tickerChan := make(chan time.Time)
	intervalChan := make(chan *interval.Interval, 1)
	stopChan := make(chan bool)

	// This is the file we iterate.  It's 3 bytes large to start,
	// so that should be the StartOffset.
	tmpFile, _ := ioutil.TempFile("/tmp", "interval_test.")
	tmpFile.Close()
	fileName = tmpFile.Name()
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123"), 0777)
	defer func() { os.Remove(tmpFile.Name()) }()

	// Start interating the file, waiting for ticks.
	i := interval.NewIter(getFilename, tickerChan, intervalChan, stopChan)
	go i.Run()

	// Send a tick to start the interval
	t1 := time.Now()
	tickerChan <-t1

	// Write more data to the file, pretend time passes...
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123456"), 0777)

	// Send a 2nd tick to finish the interval
	t2 := time.Now()
	tickerChan <-t2

	// Stop the interator and get the interval it sent us
	stopChan <-true
	got := <-intervalChan
	expect := &interval.Interval{
		FileName: fileName,
		StartTime: t1,
		StopTime: t2,
		StartOffset: 3,
		StopOffset: 6,
	}
	c.Check(got, gocheck.DeepEquals, expect)
}
