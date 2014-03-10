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
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sysconfig"
	"github.com/percona/cloud-tools/sysconfig/mysql"
	"github.com/percona/cloud-tools/test"
	"os"
	"testing"
	"time"
)

/**
 * This must be set, else all tests will fail.
 */
var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

var logChan = make(chan *proto.LogEntry, 10)
var logger = pct.NewLogger(logChan, "sysconfig-manager-test")
var tickChan = make(chan time.Time)
var sysconfigChan = make(chan *sysconfig.SystemConfig, 1)

func TestStartCollectStop(t *testing.T) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	m := mysql.NewMonitor(logger)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// First think we need is a mysql.Config.
	instance := "test1"
	config := &mysql.Config{
		DSN:          dsn,
		InstanceName: instance,
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Start the monitor.
	err = m.Start(data, tickChan, sysconfigChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// monitor=Ready once it has successfully connected to MySQL.  This may
	// take a few seconds (hopefully < 5) on a slow test machine.
	if ok := test.WaitStatus(5, m, "mysql-sysconfig", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	// Send tick to make the monitor collect.
	now := time.Now().UTC()
	tickChan <- now
	got := test.WaitSystemConfig(sysconfigChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a sysconfig after tick")
	}
	c := got[0]

	if c.Ts != now.Unix() {
		t.Error("SystemConfig.Ts set to %s; got %s", now.Unix(), c.Ts)
	}

	if len(c.Config) < 100 {
		t.Fatal("Collect > 100 vars; got %+v", c.Config)
	}

	haveWaitTimeout := false
	val := ""
	for _, s := range c.Config {
		if s[0] == instance+"/wait_timeout" {
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

	if ok := test.WaitStatus(5, m, "mysql-sysconfig", "Stopped"); !ok {
		t.Fatal("Monitor has stopped")
	}
}
