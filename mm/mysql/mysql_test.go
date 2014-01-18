package mysql_test

import (
	"log"
	"os"
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"testing"
	"time"
)

/**
 * This must be set, else all tests will fail.
 */
var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

var logChan = make(chan *proto.LogEntry, 10)
var logger = pct.NewLogger(logChan, "mm-manager-test")
var tickerChan = make(chan time.Time)
var mockTicker = mock.NewTicker(nil, tickerChan)
var collectionChan = make(chan *mm.Collection, 1)

func debug() {
	go func() {
		for logEntry := range logChan {
			log.Println(logEntry)
		}
	}()
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
/////////////////////////////////////////////////////////////////////////////

func TestStartCollectStop(t *testing.T) {
	//debug()

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
		Status:       map[string]byte{
			"Threads_connected": mm.NUMBER,
			"Threads_running": mm.NUMBER,
		},
	}
	data , err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Start the monitor.
	err = m.Start(data, mockTicker, collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// The monitor should start its ticker.
	if !mockTicker.Running {
		t.Error("Ticker is running")
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
	tickerChan <- now
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
	if c.Metrics[0].Name != prefix + "/threads_connected" {
		t.Error("First metric is ", prefix + "/threads_connected; got", c.Metrics[0].Name)
	}
	if c.Metrics[0].Number < 1 {
		t.Error("threads_connected > 1; got", c.Metrics[0].Number)
	}
	if c.Metrics[1].Name != prefix + "/threads_running" {
		t.Error("Second metric is ", prefix + "/threads_running got", c.Metrics[1].Name)
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

	if mockTicker.Running {
		t.Error("Ticker has stopped")
	}
}

func TestCollectInnoDBStats(t *testing.T) {
	//debug()
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
	 * Set up these values:
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
	if _, err := db.Exec("set global innodb_monitor_disable = '%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("set global innodb_monitor_enable = 'dml_%'"); err != nil {
		t.Fatal(err)
	}
	db.Exec("drop database if exists test_pct")
	db.Exec("create database test_pct")
	db.Exec("create table test_pct.t (i int) engine=innodb")
	db.Exec("insert into test_pct.t (i) values (42)")
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
		InnoDB: "dml_%",  // same as above ^
	}
	data , err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	err = m.Start(data, mockTicker, collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	if ok := test.WaitStatus(5, m, "mysql", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	now := time.Now()
	tickerChan <- now
	got := test.WaitCollection(collectionChan, 1)
	if len(got) == 0 {
		t.Fatal("Got a collection after tick")
	}
	c := got[0]

	/**
	 * Here's the test: monitor should have collected the InnoDB metrics.
	 */
	if len(c.Metrics) != 4 {
		t.Fatal("Collect 4 InnoDB metrics; got %+v", c.Metrics)
	}
	expect := []mm.Metric{
		{Name:"mysql/innodb/dml/dml_reads",   Type:2, Number:0},
		{Name:"mysql/innodb/dml/dml_inserts", Type:2, Number:1},
		{Name:"mysql/innodb/dml/dml_deletes", Type:2, Number:0},
		{Name:"mysql/innodb/dml/dml_updates", Type:2, Number:0},
	}
	if ok, diff := test.IsDeeply(c.Metrics, expect); !ok {
		t.Error(diff)
	}

	// Stop montior, clean up.
	m.Stop()
}
