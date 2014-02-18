package mysql_test

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
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
var logger = pct.NewLogger(logChan, "mm-manager-test")
var tickChan = make(chan time.Time)
var collectionChan = make(chan *mm.Collection, 1)

/////////////////////////////////////////////////////////////////////////////
// Test cases
/////////////////////////////////////////////////////////////////////////////

func TestStartCollectStop(t *testing.T) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	instance := "test1"
	prefix := "mysql/" + instance

	m := mysql.NewMonitor(logger)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// First think we need is a mysql.Config.
	config := &mysql.Config{
		DSN:          dsn,
		InstanceName: instance,
		Status: map[string]byte{
			"threads_connected": mm.NUMBER,
			"threads_running":   mm.NUMBER,
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Start the monitor.
	err = m.Start(data, tickChan, collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// monitor=Ready once it has successfully connected to MySQL.  This may
	// take a few seconds (hopefully < 5) on a slow test machine.
	if ok := test.WaitStatus(5, m, "mysql", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	// The monitor should only collect and send metrics on ticks; we haven't ticked yet.
	got := test.WaitCollection(collectionChan, 0)
	if len(got) > 0 {
		t.Fatal("No tick, no collection; got %+v", got)
	}

	// Now tick.  This should make monitor collect.
	now := time.Now()
	tickChan <- now
	got = test.WaitCollection(collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	if c.StartTs != now.Unix() {
		t.Error("Collection.StartTs set to %s; got %s", now.Unix(), c.StartTs)
	}

	// Only two metrics should be reported, from the config ^: Threads_connected,
	// Threads_running.  These will be prefixed and lowercase.  Their values
	// (from MySQL) are variable, but we know they should be > 1 because we're a
	// thread connected and running.
	if len(c.Metrics) != 2 {
		t.Fatal("Collected only configured metrics; got %+v", c.Metrics)
	}
	if c.Metrics[0].Name != prefix+"/threads_connected" {
		t.Error("First metric is ", prefix+"/threads_connected; got", c.Metrics[0].Name)
	}
	if c.Metrics[0].Number < 1 {
		t.Error("threads_connected > 1; got", c.Metrics[0].Number)
	}
	if c.Metrics[1].Name != prefix+"/threads_running" {
		t.Error("Second metric is ", prefix+"/threads_running got", c.Metrics[1].Name)
	}
	if c.Metrics[1].Number < 1 {
		t.Error("threads_running > 1; got", c.Metrics[0].Number)
	}

	/**
	 * Stop the monitor.
	 */

	m.Stop()

	if ok := test.WaitStatus(5, m, "mysql", "Stopped"); !ok {
		t.Fatal("Monitor has stopped")
	}
}

func TestCollectInnoDBStats(t *testing.T) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	// Get our own connection to MySQL.
	db, err := test.ConnectMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	/**
	 * Disable and reset InnoDB metrics so we can test that the monitor enables and sets them.
	 */
	if _, err := db.Exec("set global innodb_monitor_disable = '%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("set global innodb_monitor_reset_all = '%'"); err != nil {
		t.Fatal(err)
	}

	db.Exec("drop database if exists test_pct")
	db.Exec("create database test_pct")
	db.Exec("create table test_pct.t (i int) engine=innodb")
	defer db.Exec("drop database if exists test_pct")

	// Start a monitor with InnoDB metrics.
	// See TestStartCollectStop() for description of these steps.
	m := mysql.NewMonitor(logger)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	config := &mysql.Config{
		DSN:    dsn,
		Status: map[string]byte{},
		InnoDB: "dml_%", // same as above ^
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	err = m.Start(data, tickChan, collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, "mysql", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	// Do INSERT to increment dml_inserts before monitor collects.  If it enabled
	// the InnoDB metrics and collects them, we should get dml_inserts=1 this later..
	db.Exec("insert into test_pct.t (i) values (42)")

	tickChan <- time.Now()
	got := test.WaitCollection(collectionChan, 1)
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
		{Name: "mysql/innodb/dml/dml_reads", Type: 2, Number: 0},
		{Name: "mysql/innodb/dml/dml_inserts", Type: 2, Number: 1}, // <-- our INSERT
		{Name: "mysql/innodb/dml/dml_deletes", Type: 2, Number: 0},
		{Name: "mysql/innodb/dml/dml_updates", Type: 2, Number: 0},
	}
	if ok, diff := test.IsDeeply(c.Metrics, expect); !ok {
		t.Error(diff)
	}

	// Stop montior, clean up.
	m.Stop()
}

func TestCollectUserstats(t *testing.T) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	// Get our own connection to MySQL.
	db, err := test.ConnectMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	/**
	 * Disable and reset user stats.
	 */
	if _, err := db.Exec("set global userstat = off"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("flush user_statistics"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("flush index_statistics"); err != nil {
		t.Fatal(err)
	}

	// Start a monitor with user stats.
	// See TestStartCollectStop() for description of these steps.
	m := mysql.NewMonitor(logger)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	config := &mysql.Config{
		DSN:       dsn,
		UserStats: true,
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	err = m.Start(data, tickChan, collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, "mysql", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	// To get index stats, we need to use an index: mysq.user PK <host, user>
	rows, err := db.Query("select * from mysql.user where host='%' and user='msandbox'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	tickChan <- time.Now()
	got := test.WaitCollection(collectionChan, 1)
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
