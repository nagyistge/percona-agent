package mysql_test

import (
	"log"
	"os"
	"encoding/json"
	proto "github.com/percona/cloud-protocol"
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

func TestStart(t *testing.T) {
	//debug()

	instance := "test1"
	prefix := "mysql/" + instance

	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	m := mysql.NewMonitor(logger)
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// First think we need is a mysql.Config.
	config := &mysql.Config{
		DSN:          dsn,
		InstanceName: instance,
		Status:       []string{"Threads_connected", "Threads_running"},
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
	if ok := test.WaitStatus(5, m, "monitor", "Ready"); !ok {
		t.Fatal("Monitor is ready")
	}

	// The monitor should only collect and send metrics on ticks;
	// we haven't ticked yet.
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
}
