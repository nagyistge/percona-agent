/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package ticker_test

import (
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"github.com/percona/cloud-tools/ticker"
	"launchpad.net/gocheck"
	"testing"
	"time"
)

func Test(t *testing.T) { gocheck.TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Ticker test suite
/////////////////////////////////////////////////////////////////////////////

type TickerTestSuite struct{}

var _ = gocheck.Suite(&TickerTestSuite{})

// Fake time.Sleep()
var slept time.Duration

func sleep(t time.Duration) {
	slept = t
	return
}

func (s *TickerTestSuite) TestSleepTime2s(t *gocheck.C) {
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
	c := make(chan time.Time)
	et := ticker.NewEvenTicker(2, sleep)
	et.Add(c)

	// Run ticker then wait for first tick.
	go et.Run(now)
	<-c

	// Check how long ticker slept.
	got := slept.Nanoseconds()
	expect := int64(614879744)
	if got != expect {
		t.Errorf("Got %d, expected %d\n", got, expect)
	}

	et.Stop()
}

func (s *TickerTestSuite) TestSleepTime60s(t *gocheck.C) {
	// Fri Sep 27 18:11:37.385120 -0700 PDT 2013 =
	now := int64(1380330697385120263)

	c := make(chan time.Time)
	et := ticker.NewEvenTicker(60, sleep)
	et.Add(c)
	go et.Run(now)
	<-c
	got := slept.Nanoseconds()
	expect := int64(614879744 + (22 * time.Second))
	if got != expect {
		t.Errorf("Got %d, expected %d\n", got, expect)
	}
	et.Stop()
}

func (s *TickerTestSuite) TestTickerTime(t *gocheck.C) {
	/*
	 * The ticker returned by the syncer should tick at this given interval,
	 * 2s in this case.  We test this by ensuring that the current time at
	 * a couple of ticks is divisible by 2, and that the fractional seconds
	 * part is < ~1 millisecond, e.g.:
	 *   00:00:02.000123456  OK
	 *   00:00:04.000123456  OK
	 *   00:00:06.001987654  BAD
	 * This may fail on slow test machines.
	 */
	c1 := make(chan time.Time)
	c2 := make(chan time.Time)
	et := ticker.NewEvenTicker(2, time.Sleep)
	et.Add(c1)
	et.Add(c2)
	go et.Run(time.Now().UnixNano())

	// Get 2 ticks but only on c1.  "Time waits for nobody" and neither does
	// the ticker, so c2 not receiving should not affect c1.
	maxOffBy := 900000 // 900,000 ns = ~1ms
	var lastTick time.Time
	for i := 0; i < 2; i++ {
		select {
		case tick := <-c1:
			sec := tick.Second()
			if sec%2 > 0 {
				t.Errorf("Tick %d not 2s interval: %d", i, sec)
			}
			nano := tick.Nanosecond()
			if nano >= maxOffBy {
				t.Errorf("Tick %d failed: %d >= %d", i, nano, maxOffBy)
			}
			lastTick = tick
		}
	}

	// Remove c1 and recv on c2 now.  Even though c2 missed previous 2 ticks,
	// it should be able to start receiving new ticks.  By contrast, c1 should
	// not receive the tick.
	et.Remove(c1)
	timeout := time.After(2500 * time.Millisecond)
	var c2Tick time.Time
TICK_LOOP:
	for {
		select {
		case tick := <-c2:
			if !c2Tick.IsZero() {
				t.Error("c2 gets only 1 tick")
			} else {
				c2Tick = tick
			}
		case <-c1:
			t.Error("c1 does not get current tick")
		case <-timeout:
			break TICK_LOOP
		}
	}

	if c2Tick == lastTick || c2Tick.Before(lastTick) {
		t.Error("c2 gets current tick")
	}

	et.Stop()
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	tickerChan    chan time.Time
	mockTicker    *mock.Ticker
	tickerFactory *mock.TickerFactory
}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *gocheck.C) {
	s.tickerChan = make(chan time.Time)
	s.mockTicker = mock.NewTicker(nil)
	s.tickerFactory = mock.NewTickerFactory()
}

// Fake time.Sleep()
var now int64

func nowFunc() int64 {
	return now
}

func (s *ManagerTestSuite) TestAddWatcher(t *gocheck.C) {
	now = int64(1380330697385120263) // Fri Sep 27 18:11:37.385120 -0700 PDT 2013
	s.tickerFactory.Set([]ticker.Ticker{s.mockTicker})

	m := ticker.NewRolex(s.tickerFactory, nowFunc)

	c := make(chan time.Time)
	m.Add(c, 79)

	if !test.WaitState(s.mockTicker.RunningChan) {
		t.Error("Starts ticker")
	}

	if ok, diff := test.IsDeeply(s.tickerFactory.Made, []uint{79}); !ok {
		t.Errorf("Make 79s ticker, got %#v", diff)
	}

	if len(s.mockTicker.Added) == 0 {
		t.Error("Ticker added watcher")
	}

	m.Remove(c)
}
