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

package mysql_test

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/mm"
	"github.com/percona/percona-agent/mm/mysql"
	mysqlConn "github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

/**
 * This must be set, else all tests will fail.
 */
var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	db             *sql.DB
	logChan        chan *proto.LogEntry
	logger         *pct.Logger
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
	name           string
	mrm            *mock.MrmsMonitor
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	// Get our own connection to MySQL.
	db, err := test.ConnectMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.tickChan = make(chan time.Time)
	s.collectionChan = make(chan *mm.Collection, 1)
	s.name = "mm-mysql-db1"
	s.mrm = mock.NewMrmsMonitor()

}

func (s *TestSuite) TearDownSuite(t *C) {
	if s.db != nil {
		s.db.Close()
	}
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestStartCollectStop(t *C) {
	/**
	 * The mm manager uses a mm monitor factory to create monitors.  This is
	 * what the factory does...
	 */

	// First, monitors monitor an instance of some service (MySQL, RabbitMQ, etc.)
	// So the instance name and id are given.  Second, every monitor has its own
	// specific config info which is sent as the proto.Cmd.Data.  This config
	// embed a mm.Config which embed an instance.Config:
	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{
			"threads_connected": "gauge",
			"threads_running":   "gauge",
		},
	}

	// From the config, the factory determine's the monitor's name based on
	// the service instance it's monitoring, and it creates a mysql.Connector
	// for the DSN for that service (since it's a MySQL monitor in this case).
	// It creates the monitor with these args:

	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(dsn), s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// The factory returns the monitor to the manager which starts it with
	// the necessary channels:
	err := m.Start(s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// monitor=Ready once it has successfully connected to MySQL.  This may
	// take a few seconds (hopefully < 5) on a slow test machine.
	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	// The monitor should only collect and send metrics on ticks; we haven't ticked yet.
	got := test.WaitCollection(s.collectionChan, 0)
	if len(got) > 0 {
		t.Fatal("No tick, no collection; got %+v", got)
	}

	// Now tick.  This should make monitor collect.
	now := time.Now()
	s.tickChan <- now
	got = test.WaitCollection(s.collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	if c.Ts != now.Unix() {
		t.Error("Collection.Ts set to %s; got %s", now.Unix(), c.Ts)
	}

	// Only two metrics should be reported, from the config ^: threads_connected,
	// threads_running.  Their values (from MySQL) are variable, but we know they
	// should be > 1 because we're a thread connected and running.
	if len(c.Metrics) != 2 {
		t.Fatal("Collected only configured metrics; got %+v", c.Metrics)
	}
	if c.Metrics[0].Name != "mysql/threads_connected" {
		t.Error("First metric is ", "mysql/threads_connected; got", c.Metrics[0].Name)
	}
	if c.Metrics[0].Number < 1 {
		t.Error("mysql/threads_connected > 1; got", c.Metrics[0].Number)
	}
	if c.Metrics[1].Name != "mysql/threads_running" {
		t.Error("Second metric is ", "mysql/threads_running got", c.Metrics[1].Name)
	}
	if c.Metrics[1].Number < 1 {
		t.Error("mysql/threads_running > 1; got", c.Metrics[0].Number)
	}

	/**
	 * Stop the monitor.
	 */

	m.Stop()

	if ok := test.WaitStatus(5, m, s.name, "Stopped"); !ok {
		t.Fatal("Monitor has stopped")
	}
}

func (s *TestSuite) TestCollectInnoDBStats(t *C) {
	/**
	 * Disable and reset InnoDB metrics so we can test that the monitor enables and sets them.
	 */
	if _, err := s.db.Exec("set global innodb_monitor_disable = '%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("set global innodb_monitor_reset_all = '%'"); err != nil {
		t.Fatal(err)
	}

	s.db.Exec("drop database if exists percona_agent_test")
	s.db.Exec("create database percona_agent_test")
	s.db.Exec("create table percona_agent_test.t (i int) engine=innodb")
	defer s.db.Exec("drop database if exists percona_agent_test")

	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{},
		InnoDB: []string{"dml_%"}, // same as above ^
	}

	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(dsn), s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	err := m.Start(s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	// Do INSERT to increment dml_inserts before monitor collects.  If it enabled
	// the InnoDB metrics and collects them, we should get dml_inserts=1 this later..
	s.db.Exec("insert into percona_agent_test.t (i) values (42)")

	s.tickChan <- time.Now()
	got := test.WaitCollection(s.collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	/**
	 * ...monitor should have collected the InnoDB metrics:
	 *
	 * mysql> SELECT NAME, SUBSYSTEM, COUNT, TYPE FROM INFORMATION_SCHEMA.INNODB_METRICS WHERE STATUS='enabled';
	 * +-------------+-----------+-------+----------------+
	 * | NAME        | SUBSYSTEM | COUNT | TYPE           |
	 * +-------------+-----------+-------+----------------+
	 * | dml_reads   | dml       |     0 | status_counter |
	 * | dml_inserts | dml       |     1 | status_counter |
	 * | dml_deletes | dml       |     0 | status_counter |
	 * | dml_updates | dml       |     0 | status_counter |
	 * +-------------+-----------+-------+----------------+
	 */
	if len(c.Metrics) != 4 {
		t.Fatal("Collect 4 InnoDB metrics; got %+v", c.Metrics)
	}
	expect := []mm.Metric{
		{Name: "mysql/innodb/dml/dml_reads", Type: "counter", Number: 0},
		{Name: "mysql/innodb/dml/dml_inserts", Type: "counter", Number: 1}, // <-- our INSERT
		{Name: "mysql/innodb/dml/dml_deletes", Type: "counter", Number: 0},
		{Name: "mysql/innodb/dml/dml_updates", Type: "counter", Number: 0},
	}
	if ok, diff := IsDeeply(c.Metrics, expect); !ok {
		t.Error(diff)
	}

	// Stop montior, clean up.
	m.Stop()
}

func (s *TestSuite) TestCollectUserstats(t *C) {
	/**
	 * Disable and reset user stats.
	 */
	if _, err := s.db.Exec("set global userstat = off"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("flush user_statistics"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("flush index_statistics"); err != nil {
		t.Fatal(err)
	}

	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		UserStats: true,
	}

	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(dsn), s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	err := m.Start(s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	var user, host string
	err = s.db.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1) AS 'user', SUBSTRING_INDEX(CURRENT_USER(),'@',-1) AS host").Scan(&user, &host)
	if err != nil {
		t.Fatal(err)
	}

	// To get index stats, we need to use an index: mysq.user PK <host, user>
	rows, err := s.db.Query("select * from mysql.user where host=? and user=?", host, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	s.tickChan <- time.Now()
	got := test.WaitCollection(s.collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	/**
	 * Monitor should have collected the user stats: just table and index.
	 * Values vary a little, but there should be a table metric for mysql.user
	 * because login uses this table.
	 */
	if len(c.Metrics) < 1 {
		t.Fatalf("Collect at least 1 user stat metric; got %+v", c.Metrics)
	}

	var tblStat mm.Metric
	var idxStat mm.Metric
	for _, m := range c.Metrics {
		switch m.Name {
		case "mysql/db.mysql/t.user/rows_read":
			tblStat = m
		case "mysql/db.mysql/t.user/idx.PRIMARY/rows_read":
			idxStat = m
		}
	}

	// At least 2 rows should have been read from mysql.user:
	//   1: our db connection
	//   2: the monitor's db connection
	if tblStat.Number < 2 {
		t.Errorf("mysql/db.mysql/t.user/rows_read >= 2, got %+v", tblStat)
	}

	// At least 1 index read on mysql.user PK due to our SELECT ^.
	if idxStat.Number < 1 {
		t.Errorf("mysql/db.mysql/t.user/idx.PRIMARY/rows_read >= 1, got %+v", idxStat)
	}

	// Stop montior, clean up.
	m.Stop()
}

// This test is the same as TestCollectInnoDBStats with the only difference that
// now we are simulating a MySQL disconnection.
// After a disconnection, we must still be able to collect InnoDB stats
func (s *TestSuite) TestHandleMySQLRestarts(t *C) {
	/**
	 * Disable and reset InnoDB metrics so we can test that the monitor enables and sets them.
	 */
	if _, err := s.db.Exec("set global innodb_monitor_disable = '%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("set global innodb_monitor_reset_all = '%'"); err != nil {
		t.Fatal(err)
	}

	s.db.Exec("drop database if exists percona_agent_test")
	s.db.Exec("create database percona_agent_test")
	s.db.Exec("create table percona_agent_test.t (i int) engine=innodb")
	defer s.db.Exec("drop database if exists percona_agent_test")

	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{},
		InnoDB: []string{"dml_%"}, // same as above ^
	}

	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(dsn), s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	err := m.Start(s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	/**
	 * Simulate a MySQL disconnection by disabling InnoDB metrics and putting a
	 * true into the restart channel. The monitor must enable them again
	 */
	if _, err := s.db.Exec("set global innodb_monitor_disable = '%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("set global innodb_monitor_reset_all = '%'"); err != nil {
		t.Fatal(err)
	}
	s.mrm.SimulateMySQLRestart()

	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	// Do INSERT to increment dml_inserts before monitor collects.  If it enabled
	// the InnoDB metrics and collects them, we should get dml_inserts=1 this later..
	s.db.Exec("insert into percona_agent_test.t (i) values (42)")

	s.tickChan <- time.Now()
	got := test.WaitCollection(s.collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	/**
	 * ...monitor should have collected the InnoDB metrics:
	 *
	 * mysql> SELECT NAME, SUBSYSTEM, COUNT, TYPE FROM INFORMATION_SCHEMA.INNODB_METRICS WHERE STATUS='enabled';
	 * +-------------+-----------+-------+----------------+
	 * | NAME        | SUBSYSTEM | COUNT | TYPE           |
	 * +-------------+-----------+-------+----------------+
	 * | dml_reads   | dml       |     0 | status_counter |
	 * | dml_inserts | dml       |     1 | status_counter |
	 * | dml_deletes | dml       |     0 | status_counter |
	 * | dml_updates | dml       |     0 | status_counter |
	 * +-------------+-----------+-------+----------------+
	 */
	if len(c.Metrics) != 4 {
		t.Fatal("Collect 4 InnoDB metrics; got %+v", c.Metrics)
	}
	expect := []mm.Metric{
		{Name: "mysql/innodb/dml/dml_reads", Type: "counter", Number: 0},
		{Name: "mysql/innodb/dml/dml_inserts", Type: "counter", Number: 1}, // <-- our INSERT
		{Name: "mysql/innodb/dml/dml_deletes", Type: "counter", Number: 0},
		{Name: "mysql/innodb/dml/dml_updates", Type: "counter", Number: 0},
	}
	if ok, diff := IsDeeply(c.Metrics, expect); !ok {
		t.Error(diff)
	}

	// Stop montior, clean up.
	m.Stop()
}

func (s *TestSuite) TestSlowResponse(t *C) {
	// https://jira.percona.com/browse/PCT-565
	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		UserStats: true,
	}

	slowCon := mock.NewSlowMySQL(dsn)
	slowCon.SetGlobalDelay(time.Duration(config.Collect+1) * time.Second)
	m := mysql.NewMonitor(s.name, config, s.logger, slowCon, s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	err := m.Start(s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}
	defer m.Stop()

	if ok := test.WaitStatus(5, m, s.name+"-mysql", "Connected"); !ok {
		t.Fatal("Monitor is ready")
	}

	s.tickChan <- time.Now()
	got := test.WaitCollection(s.collectionChan, 1)
	// If it took more than 10% of config.Collect, the monitor must
	// discard those metrics -> len(got) == 0
	t.Check(got, HasLen, 0)
}

// Even having a wrong user/pass, the monitor should be able to start
// It won't be able to connect to the DB, but it should start without
// blocking.
// This test if for cases where the agent starts but MySQL is down
func (s *TestSuite) TestStartWithInvalidDSN(t *C) {
	config := &mysql.Config{
		Config: mm.Config{
			UUID:    "1",
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{
			"threads_connected": "gauge",
			"threads_running":   "gauge",
		},
	}

	failDsn := "user:pass@tcp(127.0.0.2:3309)/"
	m := mysql.NewMonitor(s.name, config, s.logger, mysqlConn.NewConnection(failDsn), s.mrm)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	err := m.Start(s.tickChan, s.collectionChan)
	t.Assert(err, IsNil)
}
