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
// Aggregator test suite
/////////////////////////////////////////////////////////////////////////////

type AggregatorTestSuite struct {
	logChan        chan *proto.LogEntry
	logger         *pct.Logger
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
	dataChan       chan interface{}
	spool          *mock.Spooler
}

var _ = gocheck.Suite(&AggregatorTestSuite{})

func (s *AggregatorTestSuite) SetUpSuite(t *gocheck.C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.tickChan = make(chan time.Time)
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
	a := mm.NewAggregator(s.logger, s.tickChan, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// No data, no tick, no report.
	got := test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report before tick, got: %+v", got)
	}

	// Send data.
	if err := sendCollection(sample+"/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	// Data but not tick yet, so no report.
	got = test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report before 2nd tick, got: %+v", got)
	}

	// Tick.
	t1 := time.Now()
	s.tickChan <- t1

	// Now we have data and tick, so we should have report.
	got = test.WaitMmReport(s.dataChan)
	if got == nil {
		t.Fatal("Report after 1st tick, got: %+v", got)
	}
	if got.Ts != t1 {
		t.Error("Report.Ts is 1st tick, got %s", got.Ts)
	}
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c001r.json", expect); err != nil {
		t.Fatal(err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}

	// Duration should be > 0.  We don't know exactly because it depends on
	// when this test starts and how long it takes before 1st tick, but test
	// should be sub-second so >= 3 is a reasonable sanity check.
	if got.Duration < 0 || got.Duration >= 3 {
		t.Error("Duration is close to zero, got ", got.Duration)
	}

	// Fake a tiny amount of time between ticks.
	time.Sleep(250 * time.Millisecond)

	// Data and 1st tick done, so there should not be a report any longer.
	got = test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report before 2nd tick, got: %+v", got)
	}

	// Send data and tick again.
	if err := sendCollection(sample+"/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}
	t2 := time.Now()
	s.tickChan <- t2

	// We should get 2nd report (report.Ts=t2, but same data/metrics).
	got = test.WaitMmReport(s.dataChan)
	if got == nil {
		t.Fatal("Report after 2nd tick, got: %+v", got)
	}
	if got.Ts != t2 {
		t.Error("Report.Ts is 2nd tick, got %s", got.Ts)
	}
	t.Check(got.Ts, gocheck.Equals, t2)
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}

	// Duration should be t2 - t1.
	if got.Duration != uint(t2.Sub(t1).Seconds()) {
		t.Error("Duration is t2 - t1, got", got.Duration)
	}
}

func (s *AggregatorTestSuite) TestC002(t *gocheck.C) {
	a := mm.NewAggregator(s.logger, s.tickChan, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c002-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t1 := time.Now()
	s.tickChan <- t1

	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c002r.json", expect); err != nil {
		t.Fatal("c002r.json ", err)
	}
	t.Check(got.Ts, gocheck.Equals, t1)
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

// All zero values
func (s *AggregatorTestSuite) TestC000(t *gocheck.C) {
	a := mm.NewAggregator(s.logger, s.tickChan, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	file := sample + "/c000.json"
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	t1 := time.Now()
	s.tickChan <- t1

	got := test.WaitMmReport(s.dataChan)
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c000r.json", expect); err != nil {
		t.Fatal("c000r.json ", err)
	}
	t.Check(got.Ts, gocheck.Equals, t1)
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

// COUNTER
func (s *AggregatorTestSuite) TestC003(t *gocheck.C) {
	a := mm.NewAggregator(s.logger, s.tickChan, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Tick and throw away first report to make begin ts = t1 for second report.
	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickChan <- t1
	test.WaitMmReport(s.dataChan)

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c003-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickChan <- t2

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
	t.Check(got.Ts, gocheck.Equals, t2)
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

func (s *AggregatorTestSuite) TestC003Lost(t *gocheck.C) {
	a := mm.NewAggregator(s.logger, s.tickChan, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Tick and throw away first report to make begin ts = t1 for second report.
	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickChan <- t1
	test.WaitMmReport(s.dataChan)

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
	s.tickChan <- t2

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
	t.Check(got.Ts, gocheck.Equals, t2)
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan     chan *proto.LogEntry
	logger      *pct.Logger
	mockMonitor mm.Monitor
	monitors    map[string]mm.Monitor
	tickChan    chan time.Time
	clock       *mock.Clock
	dataChan    chan interface{}
	spool       data.Spooler
	traceChan   chan string
	readyChan   chan bool
}

var _ = gocheck.Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *gocheck.C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.mockMonitor = mock.NewMonitor()

	s.monitors = map[string]mm.Monitor{"mysql": s.mockMonitor}
	s.tickChan = make(chan time.Time)

	s.traceChan = make(chan string, 10)

	s.dataChan = make(chan interface{}, 1)
	s.spool = mock.NewSpooler(s.dataChan)
}

func (s *ManagerTestSuite) SetUpTest(t *gocheck.C) {
	s.clock = mock.NewClock()
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopManager(t *gocheck.C) {

	m := mm.NewManager(s.logger, s.monitors, s.clock, s.spool)
	if m == nil {
		t.Fatal("Make new mm.Manager")
	}

	// It shouldn't be running because we haven't started it.
	if m.IsRunning() {
		t.Error("IsRunning() is false")
	}

	// It shouldn't have added a tickChan yet.
	if len(s.clock.Added) != 0 {
		t.Error("tickChan not added yet")
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

	// It should add a tickChan to the clock for the report interval.
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{60}); !ok {
		t.Errorf("Adds tickChan for report interval, got %#v", diff)
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

	status = m.Status()
	if !strings.Contains(status["Mm"], "Stopped") {
		t.Error("Status is \"Stopped\", got ", status)
	}
	if !strings.Contains(status["Mm"], `"User":"daniel"`) {
		t.Error("Status has originating cmd, got ", status)
	}
}

func (s *ManagerTestSuite) TestStartStopMonitor(t *gocheck.C) {

	// First start the manager, same as above ^ in TestStartStopManager().
	m := mm.NewManager(s.logger, s.monitors, s.clock, s.spool)
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
	// a "StartService" cmd and the monitor's config.
	mysqlConfig := &mysql.Config{
		DSN:          "user:host@tcp:(127.0.0.1:3306)",
		InstanceName: "db1",
		Status: map[string]byte{
			"threads_connected": mm.NUMBER,
			"threads_running":   mm.NUMBER,
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
		Cmd:     "StartService",
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
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{60, 1}); !ok {
		t.Errorf("Make 1s ticker for collect interval\n%s", diff)
	}

	/**
	 * Stop the monitor.
	 */

	// Starting a monitor is like starting the manager: it requires
	// a "StartService" cmd and the monitor's config.
	service = &proto.ServiceData{
		Name: "mysql",
	}
	serviceData, err = json.Marshal(service)
	if err != nil {
		t.Fatal(err)
	}
	cmd = &proto.Cmd{
		User:    "daniel",
		Cmd:     "StopService",
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

	// After stopping the monitor, the manager should remove its tickChan.
	if len(s.clock.Removed) != 1 {
		t.Error("Remove's monitor's tickChan from clock")
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
