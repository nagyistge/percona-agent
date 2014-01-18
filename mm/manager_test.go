package mm_test

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"strings"
	"testing"
	"time"
)

type managerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	mockMonitor   mm.Monitor
	monitors      map[string]mm.Monitor
	tickerChan    chan time.Time
	mockTicker    *mock.Ticker
	tickerFactory *mock.TickerFactory
	dataChan      chan interface{}
	traceChan     chan string
	readyChan     chan bool
}

var mT = &managerTestSuite{
	logChan:     make(chan *proto.LogEntry, 10),
	mockMonitor: mock.NewMonitor(),
	tickerChan:  make(chan time.Time),
	dataChan:    make(chan interface{}, 1),
}

func (mT *managerTestSuite) Setup() {
	if mT.logger == nil {
		mT.logger = pct.NewLogger(mT.logChan, "mm-manager-test")
	}
	if mT.monitors == nil {
		mT.monitors = map[string]mm.Monitor{"mysql": mT.mockMonitor}
	}
	if mT.mockTicker == nil {
		mT.mockTicker = mock.NewTicker(nil, mT.tickerChan)
	}
	if mT.tickerFactory == nil {
		mT.tickerFactory = mock.NewTickerFactory()
	}
	mT.traceChan = make(chan string, 10)
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
/////////////////////////////////////////////////////////////////////////////

func TestStartStopManager(t *testing.T) {
	mT.Setup()
	mT.tickerFactory.Set([]pct.Ticker{mT.mockTicker})

	m := mm.NewManager(mT.logger, mT.monitors, mT.tickerFactory, mT.dataChan)
	if m == nil {
		t.Fatal("Make new mm.Manager")
	}

	// It shouldn't be running because we haven't started it.
	if m.IsRunning() {
		t.Error("IsRunning() is false")
	}

	// And neither should the report ticker.
	if mT.mockTicker.Running {
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
	if ok, diff := test.IsDeeply(mT.tickerFactory.Made, []uint{60}); !ok {
		t.Errorf("Make only 60s ticker for report interval\n%s", diff)
	}

	// And it should start the aggregator which starts the ticker by calling Sync().
	if !mT.mockTicker.Running {
		t.Error("Report ticker is running")
	}

	// After starting, its status should be "Ready [cmd]" where cmd is
	// the originating ^ cmd.
	status := m.Status()
	if !strings.Contains(status["Mm"], "Ready") {
		t.Error("Status is \"Ready\", got ", status["Mm"])
	}
	if !strings.Contains(status["Mm"], "User:daniel") {
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
	if mT.mockTicker.Running {
		t.Error("Report ticker is not running")
	}
	status = m.Status()
	if !strings.Contains(status["Mm"], "Stopped") {
		t.Error("Status is \"Stopped\", got ", status)
	}
	if !strings.Contains(status["Mm"], "User:daniel") {
		t.Error("Status has originating cmd, got ", status)
	}
}

func TestStartStopMonitor(t *testing.T) {
	mT.Setup()
	collectTicker := mock.NewTicker(nil, mT.tickerChan)
	mT.tickerFactory.Set([]pct.Ticker{mT.mockTicker, collectTicker})

	// First start the manager, same as above ^ in TestStartStopManager().
	m := mm.NewManager(mT.logger, mT.monitors, mT.tickerFactory, mT.dataChan)
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
		Status:       []string{"Threads_connected", "Threads_running"},
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
	status := mT.mockMonitor.Status()
	if status["monitor"] != "Running" {
		t.Error("Monitor running")
	}

	// There should be a 60s report ticker for the aggregator and a 1s collect ticker
	// for the monitor.
	if ok, diff := test.IsDeeply(mT.tickerFactory.Made, []uint{60, 1}); !ok {
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

	status = mT.mockMonitor.Status()
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
