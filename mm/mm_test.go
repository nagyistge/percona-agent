package mm_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io/ioutil"
	"launchpad.net/gocheck"
	"os"
	"strings"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/mm"

/////////////////////////////////////////////////////////////////////////////
// Worker test suite
/////////////////////////////////////////////////////////////////////////////

type AggregatorTestSuite struct {
	tickerChan     chan time.Time
	ticker         pct.Ticker
	collectionChan chan *mm.Collection
	dataChan       chan interface{}
	spool          *mock.Spooler
}

var _ = gocheck.Suite(&AggregatorTestSuite{})

func (s *AggregatorTestSuite) SetUpSuite(t *gocheck.C) {
	s.tickerChan = make(chan time.Time)
	s.ticker = mock.NewTicker(nil, s.tickerChan)
	s.collectionChan = make(chan *mm.Collection)
	s.dataChan = make(chan interface{}, 1)
	s.spool = mock.NewSpooler(s.dataChan)
}

func sendCollection(file string, collectionChan chan *mm.Collection) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	c := &mm.Collection{}
	if err = json.Unmarshal(bytes, c); err != nil {
		return err
	}
	collectionChan <- c
	return nil
}

func (s *AggregatorTestSuite) TestC001(t *gocheck.C) {
	a := mm.NewAggregator(s.ticker, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Send load collection from file and send to aggregator.
	if err := sendCollection(sample+"/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")

	got := test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report before tick, got: %+v", got)
	}

	s.tickerChan <- t1

	got = test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report after 1st tick, got: %+v", got)
	}

	if err := sendCollection(sample+"/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	s.tickerChan <- t2

	got = test.WaitMmReport(s.dataChan)
	if got == nil {
		t.Fatal("Report after 2nd tick, got: %+v", got)
	}
	if got.Ts != t1 {
		t.Error("Report.Ts is first Unix ts, got %s", got.Ts)
	}

	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c001r.json", expect); err != nil {
		t.Fatal(err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

func (s *AggregatorTestSuite) TestC002(t *gocheck.C) {
	a := mm.NewAggregator(s.ticker, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <- t1

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c002-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <- t2

	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c002r.json", expect); err != nil {
		t.Fatal("c002r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

// All zero values
func (s *AggregatorTestSuite) TestC000(t *gocheck.C) {
	a := mm.NewAggregator(s.ticker, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <- t1

	file := sample + "/c000.json"
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <- t2

	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c000r.json", expect); err != nil {
		t.Fatal("c000r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

// COUNTER
func (s *AggregatorTestSuite) TestC003(t *gocheck.C) {
	a := mm.NewAggregator(s.ticker, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <- t1

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c003-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <- t2

	/**
	 * Pretend we're monitoring Bytes_sents every second:
	 * first val = 100
	 *           prev this diff val/s
	 * next val  100   200  100   100
	 * next val  200   400  200   200
	 * next val  400   800  400   400
	 * next val  800  1600  800   800
	 *
	 * So min bytes/s = 100, max = 800, avg = 375.  These are
	 * the values in c003r.json.
	 */
	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c003r.json", expect); err != nil {
		t.Fatal("c003r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

func (s *AggregatorTestSuite) TestC003Lost(t *gocheck.C) {
	a := mm.NewAggregator(s.ticker, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <- t1

	// The full sequence is files 1-5, but we send only 1 and 5,
	// simulating monitor failure during 2-4.  More below...
	file := fmt.Sprintf("%s/c003-1.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}
	file = fmt.Sprintf("%s/c003-5.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <- t2

	/**
	 * Values we did get are 100 and 1600 and ts 00 to 04.  So that looks like
	 * 1500 bytes / 4s = 375.  And since there was only 1 interval, we expect
	 * 375 for all stat values.
	 */
	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c003rlost.json", expect); err != nil {
		t.Fatal("c003r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	mockMonitor   mm.Monitor
	monitors      map[string]mm.Monitor
	tickerChan    chan time.Time
	mockTicker    *mock.Ticker
	tickerFactory *mock.TickerFactory
	dataChan      chan interface{}
	spool         data.Spooler
	traceChan     chan string
	readyChan     chan bool
}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *gocheck.C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.mockMonitor = mock.NewMonitor()

	s.monitors = map[string]mm.Monitor{"mysql": s.mockMonitor}
	s.tickerChan = make(chan time.Time)

	s.mockTicker = mock.NewTicker(nil, s.tickerChan)

	s.tickerFactory = mock.NewTickerFactory()

	s.traceChan = make(chan string, 10)

	s.dataChan = make(chan interface{}, 1)
	s.spool = mock.NewSpooler(s.dataChan)
}

func (s *ManagerTestSuite) TestStartStopManager(t *gocheck.C) {
	s.tickerFactory.Set([]pct.Ticker{s.mockTicker})

	m := mm.NewManager(s.logger, s.monitors, s.tickerFactory, s.spool)
	if m == nil {
		t.Fatal("Make new mm.Manager")
	}

	// It shouldn't be running because we haven't started it.
	if m.IsRunning() {
		t.Error("IsRunning() is false")
	}

	// And neither should the report ticker.
	if s.mockTicker.Running {
		t.Error("Report ticker is not running")
	}

	// First the API marshals an mm.Config.
	config := &mm.Config{
		Intervals: map[string]mm.Interval{
			"mysql": mm.Interval{Collect: 1, Report: 60},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Then it sends a StartService cmd with the config data.
	cmd := &proto.Cmd{
		User: "daniel",
		Cmd:  "StartService",
		Data: data,
	}

	// The agent calls mm.Start with the cmd (for logging and status) and the config data.
	err = m.Start(cmd, data)
	if err != nil {
		t.Fatalf("Start manager without error, got %s", err)
	}

	// Now it should be running.
	if !m.IsRunning() {
		t.Error("IsRunning() is true")
	}

	// It should start without error ^ of course, but it should also make a ticker
	// for the 60s report interval.
	if ok, diff := test.IsDeeply(s.tickerFactory.Made, []uint{60}); !ok {
		t.Errorf("Make only 60s ticker for report interval\n%s", diff)
	}

	// And it should start the aggregator which starts the ticker by calling Sync().
	if !s.mockTicker.Running {
		t.Error("Report ticker is running")
	}

	// After starting, its status should be "Ready [cmd]" where cmd is
	// the originating ^ cmd.
	status := m.Status()
	if !strings.Contains(status["Mm"], "Ready") {
		t.Error("Status is \"Ready\", got ", status["Mm"])
	}
	if !strings.Contains(status["Mm"], `"User":"daniel"`) {
		t.Error("Status has originating cmd, got ", status["Mm"])
	}

	// Starting an already started service should result in a ServiceIsRunningError.
	err = m.Start(cmd, data)
	if err == nil {
		t.Error("Start manager when already start cauess error")
	}
	switch err.(type) { // todo: ugly hack to access and test error type
	case pct.ServiceIsRunningError:
		// ok
	default:
		t.Error("Error is type pct.ServiceIsRunningError, got %T", err)
	}

	/**
	 * Stop the manager, which should "undo" all of that ^.
	 */

	err = m.Stop(cmd)

	// Repeat many of the same tests ^ but for being stopped:
	if err != nil {
		t.Fatalf("Stop manager without error, got %s", err)
	}
	if m.IsRunning() {
		t.Error("IsRunning() is false")
	}
	if s.mockTicker.Running {
		t.Error("Report ticker is not running")
	}
	status = m.Status()
	if !strings.Contains(status["Mm"], "Stopped") {
		t.Error("Status is \"Stopped\", got ", status)
	}
	if !strings.Contains(status["Mm"], `"User":"daniel"`) {
		t.Error("Status has originating cmd, got ", status)
	}
}

func (s *ManagerTestSuite) TestStartStopMonitor(t *gocheck.C) {
	collectTicker := mock.NewTicker(nil, s.tickerChan)
	s.tickerFactory.Set([]pct.Ticker{s.mockTicker, collectTicker})

	// First start the manager, same as above ^ in TestStartStopManager().
	m := mm.NewManager(s.logger, s.monitors, s.tickerFactory, s.spool)
	if m == nil {
		t.Fatal("Make new mm.Manager")
	}

	config := &mm.Config{
		Intervals: map[string]mm.Interval{
			"mysql": mm.Interval{Collect: 1, Report: 60},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	cmd := &proto.Cmd{
		User: "daniel",
		Cmd:  "StartService",
		Data: data,
	}

	err = m.Start(cmd, data)
	if err != nil {
		t.Fatalf("Start manager without error, got %s", err)
	}
	if !m.IsRunning() {
		t.Fatal("IsRunning() is true")
	}

	/**
	 * Manager is running, now we can start a monitor.
	 */

	// Starting a monitor is like starting the manager: it requires
	// a "Start" cmd and the monitor's config.
	mysqlConfig := &mysql.Config{
		DSN:          "user:host@tcp:(127.0.0.1:3306)",
		InstanceName: "db1",
		Status: map[string]byte{
			"Threads_connected": mm.NUMBER,
			"Threads_running":   mm.NUMBER,
		},
	}
	configData, err := json.Marshal(mysqlConfig)
	if err != nil {
		t.Fatal(err)
	}
	service := &proto.ServiceData{
		Name:   "mysql",
		Config: configData,
	}
	serviceData, err := json.Marshal(service)
	if err != nil {
		t.Fatal(err)
	}
	cmd = &proto.Cmd{
		User:    "daniel",
		Cmd:     "Start",
		Service: "mm",
		Data:    serviceData,
	}

	// The agent calls mm.Handle() with the cmd (for logging and status) and the config data.
	err = m.Handle(cmd)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// The monitor should be running.  The mock monitor returns "Running" if
	// Start() has been called; else it returns "Stopped".
	status := s.mockMonitor.Status()
	if status["monitor"] != "Running" {
		t.Error("Monitor running")
	}

	// There should be a 60s report ticker for the aggregator and a 1s collect ticker
	// for the monitor.
	if ok, diff := test.IsDeeply(s.tickerFactory.Made, []uint{60, 1}); !ok {
		t.Errorf("Make 1s ticker for collect interval\n%s", diff)
	}

	// The collect ticker should *not* be running yet; it's the monitor's job
	// to start it (and mock.Monitor doesn't).  By contrast, the manager does
	// start the report ticker; that's tested in StartStopManager().
	if collectTicker.Running {
		t.Error("Collect ticker not started by manager")
	}

	// Fake like the monitor starts its ticker so we can test later that
	// the manager stops it.
	collectTicker.Running = true

	/**
	 * Stop the monitor.
	 */

	// Starting a monitor is like starting the manager: it requires
	// a "Start" cmd and the monitor's config.
	service = &proto.ServiceData{
		Name: "mysql",
	}
	serviceData, err = json.Marshal(service)
	if err != nil {
		t.Fatal(err)
	}
	cmd = &proto.Cmd{
		User:    "daniel",
		Cmd:     "Stop",
		Service: "mm",
		Data:    serviceData,
	}

	err = m.Handle(cmd)
	if err != nil {
		t.Fatalf("Stop monitor without error, got %s", err)
	}

	status = s.mockMonitor.Status()
	if status["monitor"] != "Stopped" {
		t.Error("Monitor stopped")
	}

	// After stopping the monitor, the manager should also stop the ticker.
	if collectTicker.Running {
		t.Error("Collect ticker stopped by manager")
	}

	/**
	 * While we're all setup and working, let's sneak in an unknown cmd test.
	 */

	service = &proto.ServiceData{
		Name: "mysql",
	}
	serviceData, err = json.Marshal(service)
	if err != nil {
		t.Fatal(err)
	}
	cmd = &proto.Cmd{
		User:    "daniel",
		Cmd:     "Pontificate",
		Service: "mm",
		Data:    serviceData,
	}

	err = m.Handle(cmd)
	if err == nil {
		t.Fatalf("Unknown Cmd to Handle() causes error")
	}
	switch err.(type) { // todo: ugly hack to access and test error type
	case pct.UnknownCmdError:
		// ok
	default:
		t.Error("Error is type pct.UnknownCmdError, got %T", err)
	}

	/**
	 * Clean up
	 */
	m.Stop(cmd)
}
