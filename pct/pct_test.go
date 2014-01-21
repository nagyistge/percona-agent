package pct_test

import (
	"github.com/percona/cloud-tools/pct"
	"testing"
	"time"
	"os"
)

/////////////////////////////////////////////////////////////////////////////
// sys.go test suite
/////////////////////////////////////////////////////////////////////////////

func TestSameFile(t *testing.T) {
	var err error
	var same bool

	same, err = pct.SameFile("/etc/passwd", "/etc/passwd")
	if !same {
		t.Error("/etc/passwd is same as itself")
	}
	if err != nil {
		t.Error(err)
	}

	same, err = pct.SameFile("/etc/passwd", "/etc/group")
	if same {
		t.Error("/etc/passwd is same as /etc/group")
	}
	if err != nil {
		t.Error(err)
	}

	/**
	 * Simulate renaming/rotating MySQL slow log. The original slow log is renamed,
	 * then a new slow log with the same original name is created.  These two files
	 * should _not_ be the same because they'll have different inodes.
	 */
	origFile := "/tmp/pct-test"
	newFile := "/tmp/pct-test-new"
	defer func() {
		os.Remove(origFile)
		os.Remove(newFile)
	}()

	var f1 *os.File
	f1, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f1.Close()

	os.Rename(origFile, newFile)

	var f2 *os.File
	f2, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()

	same, err = pct.SameFile(origFile, newFile)
	if same {
		t.Error(origFile, "and " + newFile + " not same after rename")
	}
	if err != nil {
		t.Error(err)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ticker.go test suite
/////////////////////////////////////////////////////////////////////////////

// Fake time.Sleep()
var slept time.Duration

func sleep(t time.Duration) {
	slept = t
	return
}

func TestSleepTime2s(t *testing.T) {
	/*
	 * To sync at intervals, we must first sleep N number of nanoseconds
	 * until the next interval.  So we specify the curren time (now) in
	 * nanaseconds, and an interval time (2), and then we know how long
	 * the syncer should sleep to wait from now until the next interval
	 * time.
	 */

	// Fri Sep 27 18:11:37.385120 -0700 PDT 2013 =
	now := int64(1380330697385120263)

	// The next 2s interval, 18:11:38.000, is 0.61488 seconds away,
	// so that's how long syncer should tell our sleep func to sleep.
	et := pct.NewEvenTicker(2, sleep)
	et.Sync(now)
	got := slept.Nanoseconds()
	expect := int64(614879744)
	if got != expect {
		t.Errorf("Got %d, expected %d\n", got, expect)
	}
}

func TestSleepTime60s(t *testing.T) {
	// Fri Sep 27 18:11:37.385120 -0700 PDT 2013 =
	now := int64(1380330697385120263)

	et := pct.NewEvenTicker(60, sleep)
	et.Sync(now)
	got := slept.Nanoseconds()
	expect := int64(614879744 + (22 * time.Second))
	if got != expect {
		t.Errorf("Got %d, expected %d\n", got, expect)
	}
}

func TestTickerTime(t *testing.T) {
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
	et := pct.NewEvenTicker(2, time.Sleep)
	et.Sync(time.Now().UnixNano())
	maxOffBy := 100000
	for i := 0; i < 2; i++ {
		select {
		case tick := <-et.TickerChan():
			// 0.000 100 000
			sec := tick.Second()
			if sec%2 > 0 {
				t.Errorf("Tick %d not 2s interval: %d", i, sec)
			}
			nano := tick.Nanosecond()
			if nano < maxOffBy {
				t.Errorf("Tick %d failed: %d >= %d", i, nano, maxOffBy)
			}
		}
	}
}
