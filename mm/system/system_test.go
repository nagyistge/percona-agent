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

package system_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/system"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"path/filepath"
	"testing"
	"time"
)

func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/mm"

/////////////////////////////////////////////////////////////////////////////
// ProcStat
/////////////////////////////////////////////////////////////////////////////

type ProcStatTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ProcStatTestSuite{})

func (s *ProcStatTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")
}

// --------------------------------------------------------------------------

func (s *ProcStatTestSuite) TestProcStat001(t *C) {
	files, err := filepath.Glob(sample + "/proc/stat001-*.txt")
	if err != nil {
		t.Fatal(err)
	}

	m := system.NewMonitor(s.logger)

	metrics := []mm.Metric{}

	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := m.ProcStat(content)
		if err != nil {
			t.Fatal(err)
		}
		metrics = append(metrics, got...)
	}

	/*
					Totals		Diff
		stat001-1
			cpu		390817611
			cpu0	97641434
			cpu1	97717127
		stat001-2
			cpu		391386603	568992
			cpu0	97783608	142174	These don't add up because the real input has 4 CPU.
			cpu1	97859411	142284  This does not affect the tests.
		stat001-3
			cpu		391759882	373279
			cpu0	97876875	93267
			cpu1	97952757	93346

			1    2    3      4    5      6   7       8     9     10
			user nice system idle iowait irq softirq steal guest guestlow
	*/
	expect := []mm.Metric{
		// First input, no CPU because that requires previous values, so only cpu-ext:
		{Name: "cpu-ext/intr", Type: "counter", Number: 39222211},    // ok
		{Name: "cpu-ext/ctxt", Type: "counter", Number: 122462971},   // ok
		{Name: "cpu-ext/processes", Type: "counter", Number: 227223}, // ok
		{Name: "cpu-ext/procs_running", Type: "gauge", Number: 1},    // ok
		{Name: "cpu-ext/procs_blocked", Type: "gauge", Number: 0},    // ok
		// Second input, now we have CPU values, plus more cpu-ext values.
		{Name: "cpu/user", Type: "gauge", Number: 0.041477},    // ok 5
		{Name: "cpu/nice", Type: "gauge", Number: 0},           // ok
		{Name: "cpu/system", Type: "gauge", Number: 0.017751},  // ok 7
		{Name: "cpu/idle", Type: "gauge", Number: 99.938488},   // ok
		{Name: "cpu/iowait", Type: "gauge", Number: 0.002285},  // ok 9
		{Name: "cpu/irq", Type: "gauge", Number: 0},            // ok
		{Name: "cpu/softirq", Type: "gauge", Number: 0},        // ok 11
		{Name: "cpu/steal", Type: "gauge", Number: 0},          // ok
		{Name: "cpu/guest", Type: "gauge", Number: 0},          // ok 13
		{Name: "cpu0/user", Type: "gauge", Number: 0.131529},   // ok
		{Name: "cpu0/nice", Type: "gauge", Number: 0},          // ok 15
		{Name: "cpu0/system", Type: "gauge", Number: 0.039388}, // ok
		{Name: "cpu0/idle", Type: "gauge", Number: 99.819939},  // ok 17
		{Name: "cpu0/iowait", Type: "gauge", Number: 0.009144},
		{Name: "cpu0/irq", Type: "gauge", Number: 0}, // 19
		{Name: "cpu0/softirq", Type: "gauge", Number: 0},
		{Name: "cpu0/steal", Type: "gauge", Number: 0}, // 21
		{Name: "cpu0/guest", Type: "gauge", Number: 0},
		{Name: "cpu1/user", Type: "gauge", Number: 0.026707}, // 23
		{Name: "cpu1/nice", Type: "gauge", Number: 0},
		{Name: "cpu1/system", Type: "gauge", Number: 0.023193}, // 25
		{Name: "cpu1/idle", Type: "gauge", Number: 99.950100},
		{Name: "cpu1/iowait", Type: "gauge", Number: 0}, // 27
		{Name: "cpu1/irq", Type: "gauge", Number: 0},
		{Name: "cpu1/softirq", Type: "gauge", Number: 0}, // 29
		{Name: "cpu1/steal", Type: "gauge", Number: 0},
		{Name: "cpu1/guest", Type: "gauge", Number: 0},               // ok 31
		{Name: "cpu-ext/intr", Type: "counter", Number: 39276666},    // ok
		{Name: "cpu-ext/ctxt", Type: "counter", Number: 122631533},   // ok 33
		{Name: "cpu-ext/processes", Type: "counter", Number: 227521}, // ok
		{Name: "cpu-ext/procs_running", Type: "gauge", Number: 2},    // ok 35
		{Name: "cpu-ext/procs_blocked", Type: "gauge", Number: 0},    // ok
		// Third input.
		{Name: "cpu/user", Type: "gauge", Number: 0.038309},   // ok 37
		{Name: "cpu/nice", Type: "gauge", Number: 0},          // ok
		{Name: "cpu/system", Type: "gauge", Number: 0.017681}, // ok 39
		{Name: "cpu/idle", Type: "gauge", Number: 99.941063},
		{Name: "cpu/iowait", Type: "gauge", Number: 0.002947}, // 41
		{Name: "cpu/irq", Type: "gauge", Number: 0},
		{Name: "cpu/softirq", Type: "gauge", Number: 0}, // 43
		{Name: "cpu/steal", Type: "gauge", Number: 0},
		{Name: "cpu/guest", Type: "gauge", Number: 0}, // 45
		{Name: "cpu0/user", Type: "gauge", Number: 0.122230},
		{Name: "cpu0/nice", Type: "gauge", Number: 0}, // 47
		{Name: "cpu0/system", Type: "gauge", Number: 0.041815},
		{Name: "cpu0/idle", Type: "gauge", Number: 99.824161}, // 49
		{Name: "cpu0/iowait", Type: "gauge", Number: 0.011794},
		{Name: "cpu0/irq", Type: "gauge", Number: 0}, // 51
		{Name: "cpu0/softirq", Type: "gauge", Number: 0},
		{Name: "cpu0/steal", Type: "gauge", Number: 0}, // 53
		{Name: "cpu0/guest", Type: "gauge", Number: 0},
		{Name: "cpu1/user", Type: "gauge", Number: 0.021426},   // ok 55
		{Name: "cpu1/nice", Type: "gauge", Number: 0},          // ok
		{Name: "cpu1/system", Type: "gauge", Number: 0.024640}, // -- 57
		{Name: "cpu1/idle", Type: "gauge", Number: 99.953935},
		{Name: "cpu1/iowait", Type: "gauge", Number: 0}, // 59
		{Name: "cpu1/irq", Type: "gauge", Number: 0},
		{Name: "cpu1/softirq", Type: "gauge", Number: 0}, // 61
		{Name: "cpu1/steal", Type: "gauge", Number: 0},
		{Name: "cpu1/guest", Type: "gauge", Number: 0},               // 63
		{Name: "cpu-ext/intr", Type: "counter", Number: 39312673},    // ok
		{Name: "cpu-ext/ctxt", Type: "counter", Number: 122742465},   // 65 ok
		{Name: "cpu-ext/processes", Type: "counter", Number: 227717}, // ok
		{Name: "cpu-ext/procs_running", Type: "gauge", Number: 3},    // 67 ok
		{Name: "cpu-ext/procs_blocked", Type: "gauge", Number: 0},    // ok
	}

	if same, diff := test.IsDeeply(metrics, expect); !same {
		test.Dump(metrics)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ProcMeminfo
/////////////////////////////////////////////////////////////////////////////

type ProcMeminfoTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ProcMeminfoTestSuite{})

func (s *ProcMeminfoTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")
}

// --------------------------------------------------------------------------

func (s *ProcMeminfoTestSuite) TestProcMeminfo001(t *C) {
	m := system.NewMonitor(s.logger)
	content, err := ioutil.ReadFile(sample + "/proc/meminfo001.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ProcMeminfo(content)
	if err != nil {
		t.Fatal(err)
	}
	// Remember: the order of this array must match order in which each
	// stat appears in the input file:
	expect := []mm.Metric{
		{Name: "memory/MemTotal", Type: "gauge", Number: 8046892},  // ok
		{Name: "memory/MemFree", Type: "gauge", Number: 5273644},   // ok
		{Name: "memory/Buffers", Type: "gauge", Number: 300684},    // ok
		{Name: "memory/Cached", Type: "gauge", Number: 946852},     // ok
		{Name: "memory/SwapCached", Type: "gauge", Number: 0},      // ok
		{Name: "memory/Active", Type: "gauge", Number: 1936436},    // ok
		{Name: "memory/Inactive", Type: "gauge", Number: 598916},   // ok
		{Name: "memory/SwapTotal", Type: "gauge", Number: 8253436}, // ok
		{Name: "memory/SwapFree", Type: "gauge", Number: 8253436},  // ok
		{Name: "memory/Dirty", Type: "gauge", Number: 0},           // ok
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ProcVmstat
/////////////////////////////////////////////////////////////////////////////

type ProcVmstatTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ProcVmstatTestSuite{})

func (s *ProcVmstatTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")
}

// --------------------------------------------------------------------------

func (s *ProcVmstatTestSuite) TestProcVmstat001(t *C) {
	m := system.NewMonitor(s.logger)
	content, err := ioutil.ReadFile(sample + "/proc/vmstat001.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ProcVmstat(content)
	if err != nil {
		t.Fatal(err)
	}
	// Remember: the order of this array must match order in which each
	// stat appears in the input file:
	expect := []mm.Metric{
		{Name: "vmstat/numa_hit", Type: "counter", Number: 42594095},    // ok
		{Name: "vmstat/numa_miss", Type: "counter", Number: 0},          // ok
		{Name: "vmstat/numa_foreign", Type: "counter", Number: 0},       // ok
		{Name: "vmstat/numa_interleave", Type: "counter", Number: 7297}, // ok
		{Name: "vmstat/numa_local", Type: "counter", Number: 42594095},  // ok
		{Name: "vmstat/numa_other", Type: "counter", Number: 0},         // ok
		{Name: "vmstat/pgpgin", Type: "counter", Number: 646645},        // ok
		{Name: "vmstat/pgpgout", Type: "counter", Number: 5401659},      // ok
		{Name: "vmstat/pswpin", Type: "counter", Number: 0},             // ok
		{Name: "vmstat/pswpout", Type: "counter", Number: 0},            // ok
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ProcLoadavg
/////////////////////////////////////////////////////////////////////////////

type ProcLoadavgTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ProcLoadavgTestSuite{})

func (s *ProcLoadavgTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")
}

// --------------------------------------------------------------------------

func (s *ProcLoadavgTestSuite) TestProcLoadavg001(t *C) {
	m := system.NewMonitor(s.logger)
	content, err := ioutil.ReadFile(sample + "/proc/loadavg001.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ProcLoadavg(content)
	if err != nil {
		t.Fatal(err)
	}
	// Remember: the order of this array must match order in which each
	// stat appears in the input file:
	expect := []mm.Metric{
		{Name: "loadavg/1min", Type: "guage", Number: 0.45},     // ok
		{Name: "loadavg/5min", Type: "guage", Number: 0.56},     // ok
		{Name: "loadavg/15min", Type: "guage", Number: 0.58},    // ok
		{Name: "loadavg/running", Type: "guage", Number: 1},     // ok
		{Name: "loadavg/processes", Type: "guage", Number: 598}, // ok
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// ProcDiskstats
/////////////////////////////////////////////////////////////////////////////

type ProcDiskstatsTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ProcDiskstatsTestSuite{})

func (s *ProcDiskstatsTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")
}

// --------------------------------------------------------------------------

func (s *ProcDiskstatsTestSuite) TestProcDiskstats001(t *C) {
	m := system.NewMonitor(s.logger)
	content, err := ioutil.ReadFile(sample + "/proc/diskstats001.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.ProcDiskstats(content)
	if err != nil {
		t.Fatal(err)
	}
	// Remember: the order of this array must match order in which each
	// stat appears in the input file:
	expect := []mm.Metric{
		{Name: "disk/sda/reads", Type: "counter", Number: 56058},
		{Name: "disk/sda/reads_merged", Type: "counter", Number: 2313},
		{Name: "disk/sda/sectors_read", Type: "counter", Number: 1270506},
		{Name: "disk/sda/read_time", Type: "counter", Number: 280760},
		{Name: "disk/sda/writes", Type: "counter", Number: 232825},
		{Name: "disk/sda/writes_merged", Type: "counter", Number: 256917},
		{Name: "disk/sda/sectors_written", Type: "counter", Number: 10804063},
		{Name: "disk/sda/write_time", Type: "counter", Number: 2097320},
		{Name: "disk/sda/io_time", Type: "counter", Number: 1163068},
		{Name: "disk/sda/io_time_weighted", Type: "counter", Number: 2378728},
		{Name: "disk/sda/iops", Type: "counter", Number: 56058 + 232825},
		// --
		{Name: "disk/sda1/reads", Type: "counter", Number: 385},
		{Name: "disk/sda1/reads_merged", Type: "counter", Number: 1138},
		{Name: "disk/sda1/sectors_read", Type: "counter", Number: 4518},
		{Name: "disk/sda1/read_time", Type: "counter", Number: 4480},
		{Name: "disk/sda1/writes", Type: "counter", Number: 1},
		{Name: "disk/sda1/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/sda1/sectors_written", Type: "counter", Number: 1},
		{Name: "disk/sda1/write_time", Type: "counter", Number: 0},
		{Name: "disk/sda1/io_time", Type: "counter", Number: 2808},
		{Name: "disk/sda1/io_time_weighted", Type: "counter", Number: 4480},
		{Name: "disk/sda1/iops", Type: "counter", Number: 385 + 1},
		// --
		{Name: "disk/sda2/reads", Type: "counter", Number: 276},
		{Name: "disk/sda2/reads_merged", Type: "counter", Number: 240},
		{Name: "disk/sda2/sectors_read", Type: "counter", Number: 2104},
		{Name: "disk/sda2/read_time", Type: "counter", Number: 1692},
		{Name: "disk/sda2/writes", Type: "counter", Number: 15},
		{Name: "disk/sda2/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/sda2/sectors_written", Type: "counter", Number: 30},
		{Name: "disk/sda2/write_time", Type: "counter", Number: 0},
		{Name: "disk/sda2/io_time", Type: "counter", Number: 1592},
		{Name: "disk/sda2/io_time_weighted", Type: "counter", Number: 1692},
		{Name: "disk/sda2/iops", Type: "counter", Number: 276 + 15},
		// --
		{Name: "disk/sda3/reads", Type: "counter", Number: 55223},
		{Name: "disk/sda3/reads_merged", Type: "counter", Number: 932},
		{Name: "disk/sda3/sectors_read", Type: "counter", Number: 1262468},
		{Name: "disk/sda3/read_time", Type: "counter", Number: 270204},
		{Name: "disk/sda3/writes", Type: "counter", Number: 184397},
		{Name: "disk/sda3/writes_merged", Type: "counter", Number: 256917},
		{Name: "disk/sda3/sectors_written", Type: "counter", Number: 10804032},
		{Name: "disk/sda3/write_time", Type: "counter", Number: 1436428},
		{Name: "disk/sda3/io_time", Type: "counter", Number: 512824},
		{Name: "disk/sda3/io_time_weighted", Type: "counter", Number: 1707280},
		{Name: "disk/sda3/iops", Type: "counter", Number: 55223 + 184397},
		// --
		{Name: "disk/sr0/reads", Type: "counter", Number: 0},
		{Name: "disk/sr0/reads_merged", Type: "counter", Number: 0},
		{Name: "disk/sr0/sectors_read", Type: "counter", Number: 0},
		{Name: "disk/sr0/read_time", Type: "counter", Number: 0},
		{Name: "disk/sr0/writes", Type: "counter", Number: 0},
		{Name: "disk/sr0/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/sr0/sectors_written", Type: "counter", Number: 0},
		{Name: "disk/sr0/write_time", Type: "counter", Number: 0},
		{Name: "disk/sr0/io_time", Type: "counter", Number: 0},
		{Name: "disk/sr0/io_time_weighted", Type: "counter", Number: 0},
		{Name: "disk/sr0/iops", Type: "counter", Number: 0},
		// --
		{Name: "disk/dm-0/reads", Type: "counter", Number: 43661},
		{Name: "disk/dm-0/reads_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-0/sectors_read", Type: "counter", Number: 1094074},
		{Name: "disk/dm-0/read_time", Type: "counter", Number: 262092},
		{Name: "disk/dm-0/writes", Type: "counter", Number: 132099},
		{Name: "disk/dm-0/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-0/sectors_written", Type: "counter", Number: 5731328},
		{Name: "disk/dm-0/write_time", Type: "counter", Number: 4209168},
		{Name: "disk/dm-0/io_time", Type: "counter", Number: 231792},
		{Name: "disk/dm-0/io_time_weighted", Type: "counter", Number: 4471268},
		{Name: "disk/dm-0/iops", Type: "counter", Number: 43661 + 132099},
		// --
		{Name: "disk/dm-1/reads", Type: "counter", Number: 287},
		{Name: "disk/dm-1/reads_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-1/sectors_read", Type: "counter", Number: 2296},
		{Name: "disk/dm-1/read_time", Type: "counter", Number: 1692},
		{Name: "disk/dm-1/writes", Type: "counter", Number: 0},
		{Name: "disk/dm-1/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-1/sectors_written", Type: "counter", Number: 0},
		{Name: "disk/dm-1/write_time", Type: "counter", Number: 0},
		{Name: "disk/dm-1/io_time", Type: "counter", Number: 700},
		{Name: "disk/dm-1/io_time_weighted", Type: "counter", Number: 1692},
		{Name: "disk/dm-1/iops", Type: "counter", Number: 287 + 0},
		// --
		{Name: "disk/dm-2/reads", Type: "counter", Number: 12213},
		{Name: "disk/dm-2/reads_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-2/sectors_read", Type: "counter", Number: 165618},
		{Name: "disk/dm-2/read_time", Type: "counter", Number: 42444},
		{Name: "disk/dm-2/writes", Type: "counter", Number: 310480},
		{Name: "disk/dm-2/writes_merged", Type: "counter", Number: 0},
		{Name: "disk/dm-2/sectors_written", Type: "counter", Number: 5072704},
		{Name: "disk/dm-2/write_time", Type: "counter", Number: 1084328},
		{Name: "disk/dm-2/io_time", Type: "counter", Number: 946036},
		{Name: "disk/dm-2/io_time_weighted", Type: "counter", Number: 1126764},
		{Name: "disk/dm-2/iops", Type: "counter", Number: 12213 + 310480},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Logf("%+v\n", got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan        chan *proto.LogEntry
	logger         *pct.Logger
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "system-monitor-test")

	s.tickChan = make(chan time.Time)
	s.collectionChan = make(chan *mm.Collection, 1)
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartCollectStop(t *C) {
	if !pct.FileExists("/proc/stat") {
		t.Fatal("/proc/stat does not exist")
	}

	m := system.NewMonitor(s.logger)
	if m == nil {
		t.Fatal("Make new system.Monitor")
	}

	// First think we need is a system.Config.
	config := &system.Config{}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	// Start the monitor.
	err = m.Start(data, s.tickChan, s.collectionChan)
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// system-monitor=Ready once it has started its internals,
	// should be very fast.
	if ok := test.WaitStatus(3, m, "system-monitor", "Ready"); !ok {
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

	fmt.Printf("%+v\n", got)

	/**
	 * Stop the monitor.
	 */

	m.Stop()

	if ok := test.WaitStatus(5, m, "system-monitor", "Stopped"); !ok {
		t.Fatal("Monitor has stopped")
	}
}
