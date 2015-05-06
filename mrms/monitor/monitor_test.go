/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

package monitor_test

import (
	"testing"
	"time"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/mrms/monitor"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
	"os"
)

func Test(t *testing.T) { TestingT(t) }

var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

/////////////////////////////////////////////////////////////////////////////
// Test Suite
/////////////////////////////////////////////////////////////////////////////

type TestSuite struct {
	nullmysql *mock.NullMySQL
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.nullmysql = mock.NewNullMySQL()
	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "mrms-monitor-test")
}

func (s *TestSuite) TestStartStop(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := monitor.NewMonitor(s.logger, mockConnFactory)
	dsn := "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"

	/**
	 * Register new subscriber
	 */
	// Set initial uptime
	mockConn.SetUptime(10)
	t.Assert(mockConn.GetUptimeCount(), Equals, uint(0))
	subChan, err := m.Add(dsn)
	t.Assert(err, IsNil)
	t.Assert(mockConn.GetUptimeCount(), Equals, uint(1), Commentf("MRMS didn't checked uptime after adding first subscriber"))

	/**
	 * Start MRMS
	 */
	interval := 1 * time.Second
	err = m.Start(interval)
	t.Assert(err, IsNil)

	// Imitate MySQL restart by setting uptime to 5s (previously 10s)
	mockConn.SetUptime(5)

	// After max 1 second it should notify subscriber about MySQL restart
	var notified bool
	select {
	case notified = <-subChan:
	case <-time.After(1 * time.Second):
	}
	t.Assert(notified, Equals, true, Commentf("MySQL was restarted but MRMS didn't notify subscribers"))

	/**
	 * Stop MRMS
	 */
	err = m.Stop()
	t.Assert(err, IsNil)

	// Check status
	status := m.Status()
	t.Assert(status, DeepEquals, map[string]string{
		monitor.MONITOR_NAME: "Stopped",
	})

	// Imitate MySQL restart by setting uptime to 1s (previously 5s)
	mockConn.SetUptime(1)

	// After stopping service it should not notify subscribers anymore
	time.Sleep(2 * time.Second)
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, true, Commentf("MRMS notified subscribers after being stopped"))
}

func (s *TestSuite) TestGlobalSubscribe(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := monitor.NewMonitor(s.logger, mockConnFactory)
	dsn := "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"
	subChan, err := m.Add(dsn)
	t.Assert(err, IsNil)
	t.Assert(subChan, NotNil)

	gc, err := m.GlobalSubscribe()
	t.Assert(err, IsNil)
	t.Assert(gc, NotNil)
}
func (s *TestSuite) TestNotifications(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := monitor.NewMonitor(s.logger, mockConnFactory)
	dsn := "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"

	/**
	 * Register new subscriber
	 */
	// Set initial uptime
	mockConn.SetUptime(10)
	subChan, err := m.Add(dsn)
	t.Assert(err, IsNil)

	/**
	 * MRMS should not send notification after first check for given dsn
	 */
	var notified bool
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, false, Commentf("MySQL was not restarted (first check of MySQL server), but MRMS notified subscribers"))

	/**
	 * If MySQL was restarted then MRMS should notify subscriber
	 */
	// Imitate MySQL restart by returning 0s uptime (previously 10s)
	mockConn.SetUptime(0)
	m.Check()
	notified = false
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, true, Commentf("MySQL was restarted, but MRMS didn't notify subscribers"))

	/**
	 * If MySQL was not restarted then MRMS should not notify subscriber
	 */
	// 2s uptime is higher than previous 0s, this indicates MySQL was not restarted
	mockConn.SetUptime(2)
	m.Check()
	notified = false
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, false, Commentf("MySQL was not restarted, but MRMS notified subscribers"))

	/**
	 * Now let's imitate MySQL server restart and let's wait 3 seconds before next check.
	 * Since MySQL server was restarted and we waited 3s then uptime=3s
	 * which is higher than last registered uptime=2s
	 *
	 * However we expect in this test that this is properly detected as MySQL restart
	 * and the MRMS notifies subscribers
	 */
	waitTime := int64(3)
	time.Sleep(time.Duration(waitTime) * time.Second)
	mockConn.SetUptime(waitTime)
	m.Check()
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, true, Commentf("MySQL was restarted (uptime overlaped last registered uptime), but MRMS didn't notify subscribers"))

	/**
	 * After removing subscriber MRMS should not notify it anymore about MySQL restarts
	 */
	// Imitate MySQL restart by returning 0s uptime (previously 3s)
	mockConn.SetUptime(0)
	m.Remove(dsn, subChan)
	m.Check()
	notified = false
	select {
	case notified = <-subChan:
	default:
	}
	t.Assert(notified, Equals, false, Commentf("Subscriber was removed but MRMS still notified it about MySQL restart"))
}

func (s *TestSuite) TestSubscribers(t *C) {
	subs := monitor.NewSubscribers(s.logger)
	rwChan := make(chan string, 100)
	dsn := "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"
	err := subs.GlobalAdd(rwChan, dsn)
	t.Assert(err, Equals, nil)

	err = subs.GlobalAdd(rwChan, "")
	t.Assert(err, NotNil)
}

func (s *TestSuite) Test2Subscribers(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := monitor.NewMonitor(s.logger, mockConnFactory)
	dsn := "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"

	c1, err := m.Add(dsn)
	t.Assert(err, IsNil)

	c2, err := m.Add(dsn)
	t.Assert(err, IsNil)

	var notified bool

	mockConn.SetUptime(1)
	m.Check()
	select {
	case notified = <-c1:
	default:
	}
	t.Check(notified, Equals, false)
	select {
	case notified = <-c2:
	default:
	}
	t.Check(notified, Equals, false)

	mockConn.SetUptime(2)
	m.Check()
	select {
	case notified = <-c1:
	default:
	}
	t.Check(notified, Equals, false)
	select {
	case notified = <-c2:
	default:
	}
	t.Check(notified, Equals, false)
}

func (s *TestSuite) TestRealMySQL(t *C) {
	if dsn == "" {
		t.Skip("PCT_TEST_MYSQL_DSN is not set")
	}
	m := monitor.NewMonitor(s.logger, &mysql.RealConnectionFactory{})
	c, err := m.Add(dsn)
	t.Assert(err, IsNil)
	defer m.Remove(dsn, c)
	for i := 0; i < 2; i++ {
		time.Sleep(1 * time.Second)
		m.Check()
		select {
		case <-c:
			t.Logf("False-positive restart reported on check number %d", i)
			t.FailNow()
		default:
		}
	}
}
