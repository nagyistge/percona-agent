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

package mm_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/data"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mm"
	"github.com/percona/percona-agent/mm/mysql"
	"github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/mm/metrics"

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

var _ = Suite(&AggregatorTestSuite{})

func (s *AggregatorTestSuite) SetUpSuite(t *C) {
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

// --------------------------------------------------------------------------

func (s *AggregatorTestSuite) TestGoTime(t *C) {
	t0, _ := time.Parse("2006-01-02T15:04:05", "2014-01-01T12:00:00")
	t.Check(mm.GoTime(120, 1388577600), Equals, t0) // 12:00:00
	t.Check(mm.GoTime(120, 1388577601), Equals, t0) // 12:00:01
	t.Check(mm.GoTime(120, 1388577660), Equals, t0) // 12:01:00
	t.Check(mm.GoTime(120, 1388577719), Equals, t0) // 12:01:59

	t1, _ := time.Parse("2006-01-02T15:04:05", "2014-01-01T12:02:00")
	t.Check(mm.GoTime(120, 1388577720), Equals, t1) // 12:02:00
}

func (s *AggregatorTestSuite) TestC001(t *C) {
	interval := int64(300)
	a := mm.NewAggregator(s.logger, interval, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Load collection from file and send to aggregator.
	if err := sendCollection(sample+"/c001-1.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	// Ts in c001 is 2009-11-10 23:00:00.
	t1, _ := time.Parse("2006-01-02 15:04:05", "2009-11-10 23:00:00")

	got := test.WaitMmReport(s.dataChan)
	if got != nil {
		t.Error("No report before 2nd interval, got: %+v", got)
	}

	// Ts in c001 is 2009-11-10 23:05:01, 1s into the next interval.
	if err := sendCollection(sample+"/c001-2.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	got = test.WaitMmReport(s.dataChan)
	t.Assert(got, NotNil)
	t.Check(got.Ts, Equals, t1)
	t.Check(uint64(got.Duration), Equals, uint64(interval))

	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c001r.json", expect); err != nil {
		t.Fatal(err)
	}
	t.Check(got.Ts, Equals, t1)
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		test.Dump(got.Stats)
		test.Dump(expect.Stats)
		t.Fatal(diff)
	}

}

func (s *AggregatorTestSuite) TestC002(t *C) {
	interval := int64(300)
	a := mm.NewAggregator(s.logger, interval, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Ts in c002-1 is 2009-11-10 23:00:00.
	t1, _ := time.Parse("2006-01-02 15:04:05", "2009-11-10 23:00:00")

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c002-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}
	// Next interval causes 1st to be reported.
	file := fmt.Sprintf("%s/c002-n.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	got := test.WaitMmReport(s.dataChan)
	t.Assert(got, NotNil)
	t.Check(got.Ts, Equals, t1)
	t.Check(uint64(got.Duration), Equals, uint64(interval))

	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c002r.json", expect); err != nil {
		t.Fatal("c002r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		t.Fatal(diff)
	}
}

// All zero values
func (s *AggregatorTestSuite) TestC000(t *C) {
	interval := int64(60)
	a := mm.NewAggregator(s.logger, interval, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Ts in c000 is 2009-11-10 23:00:00.
	t1, _ := time.Parse("2006-01-02 15:04:05", "2009-11-10 23:00:00")

	file := sample + "/c000.json"
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}
	file = sample + "/c000-n.json"
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	got := test.WaitMmReport(s.dataChan)
	t.Assert(got, NotNil)
	t.Check(got.Ts, Equals, t1)
	t.Check(uint64(got.Duration), Equals, uint64(interval))

	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c000r.json", expect); err != nil {
		t.Fatal("c000r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		t.Fatal(diff)
	}
}

// COUNTER
func (s *AggregatorTestSuite) TestC003(t *C) {
	interval := int64(5)
	a := mm.NewAggregator(s.logger, interval, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Ts in c003 is 2009-11-10 23:00:00.
	t1, _ := time.Parse("2006-01-02 15:04:05", "2009-11-10 23:00:00")

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c003-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}
	// Next interval causes 1st to be reported.
	file := fmt.Sprintf("%s/c003-n.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

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
	t.Assert(got, NotNil)
	t.Check(got.Ts, Equals, t1)
	t.Check(uint64(got.Duration), Equals, uint64(interval))
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c003r.json", expect); err != nil {
		t.Fatal("c003r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		t.Fatal(diff)
	}

	// Get the collected stats
	// As got.Stats[0].Stats is a map, we run this empty 'for' loop just to get
	// the stats for the first key in the map, into the stats variable.
	var stats *mm.Stats
	for _, stats = range got.Stats[0].Stats {
	}
	// First time, stats.Cnt must be equal to the number of seconds in the interval
	// minus 1 because the first value is used to bootstrap the aggregator
	t.Check(int64(stats.Cnt), Equals, interval-1)

	// Let's complete the second interval
	for i := 6; i <= 9; i++ {
		file := fmt.Sprintf("%s/c003-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}
	// Sample #10 will be in the 3rd interval, so the 2nd will be reported
	file = fmt.Sprintf("%s/c003-%d.json", sample, 10)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	got = test.WaitMmReport(s.dataChan)
	t.Assert(got, NotNil)
	// Get the collected stats
	for _, stats = range got.Stats[0].Stats {
	}
	// stats.Cnt must be equal to the number of seconds in the interval
	t.Check(int64(stats.Cnt), Equals, interval)
	if err := test.LoadMmReport(sample+"/c003r2.json", expect); err != nil {
		t.Fatal("c003r2.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		t.Fatal(diff)
	}
}

func (s *AggregatorTestSuite) TestC003Lost(t *C) {
	interval := int64(5)
	a := mm.NewAggregator(s.logger, interval, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	// Ts in c003 is 2009-11-10 23:00:00.
	t1, _ := time.Parse("2006-01-02 15:04:05", "2009-11-10 23:00:00")

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
	// Next interval causes 1st to be reported.
	file = fmt.Sprintf("%s/c003-n.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	/**
	 * Values we did get are 100 and 1600 and ts 00 to 04.  So that looks like
	 * 1500 bytes / 4s = 375.  And since there was only 1 interval, we expect
	 * 375 for all stat values.
	 */
	got := test.WaitMmReport(s.dataChan)
	t.Assert(got, NotNil)
	t.Check(got.Ts, Equals, t1)
	t.Check(uint64(got.Duration), Equals, uint64(interval))
	expect := &mm.Report{}
	if err := test.LoadMmReport(sample+"/c003rlost.json", expect); err != nil {
		t.Fatal("c003r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Stats, expect.Stats); !ok {
		test.Dump(got.Stats)
		test.Dump(expect.Stats)
		t.Fatal(diff)
	}
}

func (s *AggregatorTestSuite) TestBadMetric(t *C) {
	/**
	 * Bad metrics should not exist and certainly not aggregated because they
	 * can go undetected for a long time because they'll result in zero values
	 * which are valid in normal cases.  The metric is bad in the input because
	 * its type is "guage" instead of "gauge", and it's the only metric so the
	 * result should be zero metrics.
	 */
	a := mm.NewAggregator(s.logger, 60, s.collectionChan, s.spool)
	go a.Start()
	defer a.Stop()

	file := fmt.Sprintf("%s/bad_metric.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}
	file = fmt.Sprintf("%s/bad_metric-n.json", sample)
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	got := test.WaitMmReport(s.dataChan)
	t.Check(len(got.Stats), Equals, 1)          // instance
	t.Check(len(got.Stats[0].Stats), Equals, 0) // ^ its metrics
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	tickChan      chan time.Time
	clock         *mock.Clock
	dataChan      chan interface{}
	spool         data.Spooler
	traceChan     chan string
	readyChan     chan bool
	tmpDir        string
	configDir     string
	im            *instance.Repo
	mysqlMonitor  *mock.MmMonitor
	systemMonitor *mock.MmMonitor
	factory       *mock.MmMonitorFactory
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "mm-manager-test")
	s.tickChan = make(chan time.Time)
	s.traceChan = make(chan string, 10)
	s.dataChan = make(chan interface{}, 1)
	s.spool = mock.NewSpooler(s.dataChan)

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "im"), s.configDir, s.api)
	data, err := json.Marshal(&proto.MySQLInstance{
		Hostname: "db1",
		DSN:      "user:host@tcp:(127.0.0.1:3306)",
	})
	t.Assert(err, IsNil)
	s.im.Add("mysql", 1, data, false)
	data, err = json.Marshal(&proto.ServerInstance{Hostname: "host1"})
	t.Assert(err, IsNil)
	s.im.Add("server", 1, data, false)

	s.mysqlMonitor = mock.NewMmMonitor()
	s.systemMonitor = mock.NewMmMonitor()
	s.factory = mock.NewMmMonitorFactory(map[string]mm.Monitor{
		"mysql-1":  s.mysqlMonitor,
		"server-1": s.systemMonitor,
	})

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	s.clock = mock.NewClock()
	glob := filepath.Join(pct.Basedir.Dir("config"), "*")
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopManager(t *C) {
	/**
	 * mm is a proxy manager for monitors, so it's always running.
	 * It should implement the service manager interface anyway,
	 * but it doesn't actually start or stop.  Its main work is done
	 * in Handle, starting and stopping monitors (tested later).
	 */
	m := mm.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	if m == nil {
		t.Fatal("Make new mm.Manager")
	}

	// It shouldn't have added a tickChan yet.
	if len(s.clock.Added) != 0 {
		t.Error("tickChan not added yet")
	}

	// First the API marshals an mm.Config.
	config := &mm.Config{
		ServiceInstance: proto.ServiceInstance{
			Service:    "mysql",
			InstanceId: 1,
		},
		Collect: 1,
		Report:  60,
		// No monitor-specific config
	}
	err := pct.Basedir.WriteConfig("mm-mysql-1", config)
	t.Assert(err, IsNil)

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// There is a monitor so there should be tickers.
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{1}); !ok {
		test.Dump(s.clock.Added)
		t.Errorf("Does not add tickChan, got %#v", diff)
	}

	// Its status should be "Running".
	status := m.Status()
	t.Check(status["mm"], Equals, "Running")

	// Can't start mm twice.
	err = m.Start()
	t.Check(err, Not(Equals), "")

	// Stopping should be idempotent.
	err = m.Stop()
	t.Check(err, IsNil)
	err = m.Stop()
	t.Check(err, IsNil)

	status = m.Status()
	t.Check(status["mm"], Equals, "Stopped")
}

/**
 * Tests:
 * - starting monitor
 * - stopping monitor
 * - starting monitor again (restarting monitor)
 * - sneaked in:) unknown cmd test
 */
func (s *ManagerTestSuite) TestRestartMonitor(t *C) {
	// Create and start mm, no monitors yet.
	m := mm.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Assert(err, IsNil)

	// Start a monitor by sending StartService + monitor config.
	// This is the config in test/mm/config/mm-mysql-1.conf.
	mmConfig := &mysql.Config{
		Config: mm.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{
			"threads_connected": "gauge",
			"threads_running":   "gauge",
		},
	}
	mmConfigData, err := json.Marshal(mmConfig)
	t.Assert(err, IsNil)

	// If this were a real monitor, it would decode and set its own config.
	// The mock monitor doesn't have any real config type, so we set it manually.
	s.mysqlMonitor.SetConfig(mmConfig)

	// The agent calls mm.Handle() with the cmd (for logging and status) and the config data.
	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "StartService",
		Data:    mmConfigData,
	}
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	// The monitor should be running.  The mock monitor returns "Running" if
	// Start() has been called; else it returns "Stopped".
	status := m.Status()
	t.Check(status["monitor"], Equals, "Running")

	// There should be a 1s collect ticker for the monitor.
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{1}); !ok {
		t.Errorf("Make 1s ticker for collect interval\n%s", diff)
	}

	// After starting a monitor, mm should write its config to the dir
	// it learned when mm.LoadConfig() was called.  Next time agent starts,
	// it will have mm start the monitor with this config.
	data, err := ioutil.ReadFile(s.configDir + "/mm-mysql-1.conf")
	t.Check(err, IsNil)
	gotConfig := &mysql.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := test.IsDeeply(gotConfig, mmConfig); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}

	/**
	 * Stop the monitor.
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "StopService",
		Data:    mmConfigData,
	}

	// Handles StopService without error.
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	// Stop a monitor removes it from the managers list of monitors.
	// So it's no longer present in a status request.
	status = m.Status()
	t.Check(status["monitor"], Equals, "")

	// After stopping the monitor, the manager should remove its tickChan.
	if len(s.clock.Removed) != 1 {
		t.Error("Remove's monitor's tickChan from clock")
	}

	// After stopping a monitor, mm should remove its config file so agent
	// doesn't start it on restart.
	file := s.configDir + "/mm-mysql-1.conf"
	if pct.FileExists(file) {
		t.Error("Stopping monitor removes its config; ", file, " exists")
	}

	/**
	 * Start the monitor again (restarting monitor).
	 */
	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "StartService",
		Data:    mmConfigData,
	}

	// If this were a real monitor, it would decode and set its own config.
	// The mock monitor doesn't have any real config type, so we set it manually.
	s.mysqlMonitor.SetConfig(mmConfig)

	// The agent calls mm.Handle() with the cmd (for logging and status) and the config data.
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	// The monitor should be running.  The mock monitor returns "Running" if
	// Start() has been called; else it returns "Stopped".
	status = m.Status()
	t.Check(status["monitor"], Equals, "Running")

	// There should be a 1s collect ticker for the monitor.
	// (Actually two in s.clock.Added, as this is mock and we started monitor twice)
	if ok, diff := test.IsDeeply(s.clock.Added, []uint{1, 1}); !ok {
		t.Errorf("Make 1s ticker for collect interval\n%s", diff)
	}

	// After starting a monitor, mm should write its config to the dir
	// it learned when mm.LoadConfig() was called.  Next time agent starts,
	// it will have mm start the monitor with this config.
	data, err = ioutil.ReadFile(s.configDir + "/mm-mysql-1.conf")
	t.Check(err, IsNil)
	gotConfig = &mysql.Config{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := test.IsDeeply(gotConfig, mmConfig); !same {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	/**
	 * While we're all setup and working, let's sneak in an unknown cmd test.
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "Pontificate",
		Data:    mmConfigData,
	}

	// Unknown cmd causes error.
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Not(Equals), "")
}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	m := mm.NewManager(s.logger, s.factory, s.clock, s.spool, s.im)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Assert(err, IsNil)

	/**
	 * Start a mock MySQL monitor.
	 */
	mysqlMonitorConfig := &mysql.Config{
		Config: mm.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
			Collect: 1,
			Report:  60,
		},
		Status: map[string]string{
			"threads_connected": "gauge",
			"threads_running":   "gauge",
		},
	}
	mysqlData, err := json.Marshal(mysqlMonitorConfig)
	t.Assert(err, IsNil)
	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "StartService",
		Data:    mysqlData,
	}
	s.mysqlMonitor.SetConfig(mysqlMonitorConfig)
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Assert(reply.Error, Equals, "")

	/**
	 * Start a mock system monitor.
	 */
	systemMonitorConfig := &system.Config{
		Config: mm.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "server",
				InstanceId: 1,
			},
			Collect: 10,
			Report:  60,
		},
	}
	systemData, err := json.Marshal(systemMonitorConfig)
	t.Assert(err, IsNil)
	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "mm",
		Cmd:     "StartService",
		Data:    systemData,
	}
	s.systemMonitor.SetConfig(systemMonitorConfig)
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Assert(reply.Error, Equals, "")

	/**
	 * GetConfig from mm which should return all monitors' configs.
	 */
	cmd = &proto.Cmd{
		Cmd:     "GetConfig",
		Service: "mm",
	}
	reply = m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfig := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfig); err != nil {
		t.Fatal(err)
	}
	expectConfig := []proto.AgentConfig{
		{
			InternalService: "mm",
			ExternalService: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
			Config:  string(mysqlData),
			Running: true,
		},
		{
			InternalService: "mm",
			ExternalService: proto.ServiceInstance{
				Service:    "server",
				InstanceId: 1,
			},
			Config:  string(systemData),
			Running: true,
		},
	}
	if same, diff := test.IsDeeply(gotConfig, expectConfig); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}
}
