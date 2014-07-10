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

package mysql_test

import (
	"github.com/percona/cloud-protocol/proto"
	mysqlConn "github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/sysconfig"
	"github.com/percona/percona-agent/sysconfig/mysql"
	"github.com/percona/percona-agent/test"
	. "gopkg.in/check.v1"
	"os"
	"testing"
	"time"
)

/**
 * This must be set, else all tests will fail.
 */
var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logChan    chan *proto.LogEntry
	logger     *pct.Logger
	tickChan   chan time.Time
	reportChan chan *sysconfig.Report
	name       string
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.tickChan = make(chan time.Time)
	s.reportChan = make(chan *sysconfig.Report, 1)
	s.reportChan = make(chan *sysconfig.Report, 1)
	s.name = "sysconfig-mysql-db1"
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestStartCollectStop(t *C) {
	// Create the monitor.
	config := &mysql.Config{
		Config: sysconfig.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
		},
	}
	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(dsn))
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// Start the monitor.
	err := m.Start(s.tickChan, s.reportChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// monitor=Ready once it has successfully connected to MySQL.  This may
	// take a few seconds (hopefully < 5) on a slow test machine.
	if ok := test.WaitStatusPrefix(5, m, s.name, "Idle"); !ok {
		t.Fatal("Monitor is ready")
	}

	// Send tick to make the monitor collect.
	now := time.Now().UTC()
	s.tickChan <- now
	got := test.WaitSystemConfig(s.reportChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a sysconfig after tick")
	}
	c := got[0]

	if c.Ts != now.Unix() {
		t.Error("Report.Ts set to %s; got %s", now.Unix(), c.Ts)
	}

	if len(c.Settings) < 100 {
		t.Fatal("Collect > 100 vars; got %+v", c.Settings)
	}

	haveWaitTimeout := false
	val := ""
	for _, s := range c.Settings {
		if s[0] == "wait_timeout" {
			haveWaitTimeout = true
			val = s[1]
		}
	}
	if !haveWaitTimeout {
		t.Logf("%+v\n", c)
		t.Error("Got wait_timeout")
	}
	if val == "" {
		t.Error("wait_timeout has value")
	}

	/**
	 * Stop the monitor.
	 */

	m.Stop()

	if ok := test.WaitStatus(5, m, s.name, "Stopped"); !ok {
		t.Fatal("Monitor has stopped")
	}
}
