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

package system

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/pct"
	"io/ioutil"
	"strconv"
	"strings"
	"time"
)

// Keep CPUStates and nCPUStates in sync, else Go will panic accessing index out of range.
var CPUStates []string = []string{"user", "nice", "system", "idle", "iowait", "irq", "softirq", "steal", "guest", "guestlow"}

const nCPUStates = 10

type Monitor struct {
	name   string
	logger *pct.Logger
	config *Config
	// --
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
	// --
	prevCPUval map[string][]float64 // [cpu0] => [user, nice, ...]
	prevCPUsum map[string]float64   // [cpu0] => user + nice + ...
	sync       *pct.SyncChan
	status     *pct.Status
	running    bool
}

func NewMonitor(name string, config *Config, logger *pct.Logger) *Monitor {
	m := &Monitor{
		name:   name,
		config: config,
		logger: logger,
		// --
		prevCPUval: make(map[string][]float64),
		prevCPUsum: make(map[string]float64),
		status:     pct.NewStatus([]string{name}),
		sync:       pct.NewSyncChan(),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(tickChan chan time.Time, collectionChan chan *mm.Collection) error {
	m.logger.Debug("Start:call")
	defer m.logger.Debug("Start:return")

	if m.running {
		return pct.ServiceIsRunningError{m.name}
	}

	m.tickChan = tickChan
	m.collectionChan = collectionChan

	go m.run()
	m.running = true
	m.logger.Info("Started")

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	m.logger.Debug("Stop:call")
	defer m.logger.Debug("Stop:return")

	if m.config == nil {
		return nil // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update(m.name, "Stopping")
	m.sync.Stop()
	m.sync.Wait()

	m.config = nil // no config if not running
	m.running = false
	m.logger.Info("Stopped")

	// Do not update status to "Stopped" here; run() does that on return.
	return nil
}

// @goroutine[0]
func (m *Monitor) Status() map[string]string {
	return m.status.All()
}

// @goroutine[0]
func (m *Monitor) TickChan() chan time.Time {
	return m.tickChan
}

// @goroutine[0]
func (m *Monitor) Config() interface{} {
	return m.config
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func StrToFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		f = 0
	}
	return f
}

func (m *Monitor) run() {
	m.logger.Debug("run:call")
	defer func() {
		m.status.Update(m.name, "Stopped")
		m.sync.Done()
		m.logger.Debug("run:return")
	}()

	var lastTs int64
	for {
		m.logger.Debug("run:wait")
		m.status.Update(m.name, fmt.Sprintf("Idle (last collected at %s)", time.Unix(lastTs, 0)))
		select {
		case now := <-m.tickChan:
			m.logger.Debug("run:collect:start")
			m.status.Update(m.name, "Running")

			c := &mm.Collection{
				ServiceInstance: proto.ServiceInstance{
					Service:    m.config.Service,
					InstanceId: m.config.InstanceId,
				},
				Ts:      now.UTC().Unix(),
				Metrics: []mm.Metric{},
			}

			content, err := ioutil.ReadFile("/proc/stat")
			if err == nil {
				if metrics, err := m.ProcStat(content); err != nil {
					m.logger.Warn("system:run:ProcStat:", err)
				} else {
					c.Metrics = append(c.Metrics, metrics...)
				}
			}

			content, err = ioutil.ReadFile("/proc/meminfo")
			if err == nil {
				if metrics, err := m.ProcMeminfo(content); err != nil {
					m.logger.Warn("system:run:ProcMeminfo:", err)
				} else {
					c.Metrics = append(c.Metrics, metrics...)
				}
			}

			content, err = ioutil.ReadFile("/proc/vmstat")
			if err == nil {
				if metrics, err := m.ProcVmstat(content); err != nil {
					m.logger.Warn("system:run:ProcVmstat:", err)
				} else {
					c.Metrics = append(c.Metrics, metrics...)
				}
			}

			content, err = ioutil.ReadFile("/proc/loadavg")
			if err == nil {
				if metrics, err := m.ProcLoadavg(content); err != nil {
					m.logger.Warn("system:run:ProcLoadavg:", err)
				} else {
					c.Metrics = append(c.Metrics, metrics...)
				}
			}

			content, err = ioutil.ReadFile("/proc/diskstats")
			if err == nil {
				if metrics, err := m.ProcDiskstats(content); err != nil {
					m.logger.Warn("system:run:ProcDiskstats:", err)
				} else {
					c.Metrics = append(c.Metrics, metrics...)
				}
			}

			// Send the metrics to the aggregator.
			if len(c.Metrics) > 0 {
				select {
				case m.collectionChan <- c:
					lastTs = c.Ts
				case <-time.After(500 * time.Millisecond):
					// lost collection
					m.logger.Debug("Lost system metrics; timeout spooling after 500ms")
				}
			} else {
				m.logger.Debug("run:no metrics") // shouldn't happen
			}

			m.logger.Debug("run:collect:stop")
		case <-m.sync.StopChan:
			m.logger.Debug("run:stop")
			return
		}
	}
}

func (m *Monitor) ProcStat(content []byte) ([]mm.Metric, error) {
	m.logger.Debug("ProcStat:call")
	defer m.logger.Debug("ProcStat:return")

	m.status.Update(m.name, "Getting /proc/stat metrics")

	metrics := []mm.Metric{}
	currCPUval := make(map[string][]float64)
	currCPUsum := make(map[string]float64)

	lines := strings.Split(string(content), "\n")
	for _, v := range lines {
		fields := strings.Fields(v)
		if len(fields) < 2 { // at least two fields expected
			continue
		}

		if strings.HasPrefix(fields[0], "cpu") {
			/**
			 *   cpu  139353 5811 67733 357492400 100037 0 355 0 0 0
			 *   cpu0 95077 2434 30487 89168743 96762 0 81 0 0 0
			 *   cpu1 28682 2183 19588 89411103 866 0 99 0 0 0
			 *   cpu2 11110 667 12338 89450607 1875 0 138 0 0 0
			 *   cpu3 4482 525 5318 89461946 532 0 35 0 0 0
			 *
			 * Fields 1-10 map to CPUStates (see top of this file).  Each field
			 * is "The amount of time, measured in units of USER_HZ (1/100ths of
			 * a second on most architectures ... that the system spent in various
			 * states" (http://man7.org/linux/man-pages/man5/proc.5.html).  So
			 * individually the fields are meaningless to us.  We add them all
			 * for total CPU time, then report the percentage of each/total CPU time.
			 * E.g. if all fields add to 1000, and first field (user state) is 500,
			 * then CPU spent 50% of time in user state.  To complicate things more:
			 * /proc/stat is a counter not a gauge, meaning that values increase
			 * monotonically.  So a single measurement is also meaningless: we must
			 * calc the diff, current - prev, and compare values to this.  E.g. if
			 * prev total/user was 1000/500 and current total/user is 2000/550, then
			 * total/user diff is +1000/50, meaning that during that interval
			 * CPU was in user state 50 / 1000 * 100 = 5% of the time.  Another way
			 * to think about it: "from prev to now CPU was busy for 1000 clock ticks
			 * of which 50 (5%) were in user state."
			 */

			cpu := fields[0]

			for state, valStr := range fields {
				if state == 0 || state >= nCPUStates {
					continue
				}
				val := StrToFloat(valStr) // "1.2" -> 1.2
				if _, ok := currCPUval[cpu]; !ok {
					currCPUval[cpu] = make([]float64, nCPUStates)
				}
				currCPUval[cpu][state] = val
				currCPUsum[cpu] += val
			}

			if _, ok := m.prevCPUsum[cpu]; ok {
				for state, _ := range fields {
					if state == 0 || state >= nCPUStates {
						continue
					}
					if currCPUsum[cpu] > m.prevCPUsum[cpu] {
						m := mm.Metric{
							Name:   cpu + "/" + CPUStates[state-1],
							Type:   "gauge",
							Number: (currCPUval[cpu][state] - m.prevCPUval[cpu][state]) * 100 / (currCPUsum[cpu] - m.prevCPUsum[cpu]),
						}
						metrics = append(metrics, m)
					}
				}
			}

		} else {
			// Not a cpu line; another metric we want?
			switch fields[0] {
			case "intr", "ctxt", "processes":
				/**
				 * http://man7.org/linux/man-pages/man5/proc.5.html
				 * intr
				 *   This line shows counts of interrupts serviced since
				 *   boot time, for each of the possible system interrupts.
				 *   The first column is the total of all interrupts
				 *   serviced; each subsequent column is the total for a
				 *   particular interrupt.
				 * ctxt
				 *   The number of context switches that the system underwent.
				 * proccess
				 *   Number of forks since boot.
				 */
				m := mm.Metric{
					Name:   "cpu-ext/" + fields[0],
					Type:   "counter",
					Number: StrToFloat(fields[1]),
				}
				metrics = append(metrics, m)
			case "procs_running", "procs_blocked":
				/**
				 * http://man7.org/linux/man-pages/man5/proc.5.html
				 * Number of processes in runnable state.  (Linux 2.5.45 onward.)
				 * Number of processes blocked waiting for I/O to complete.  (Linux 2.5.45 onward.)
				 */
				m := mm.Metric{
					Name:   "cpu-ext/" + fields[0],
					Type:   "gauge",
					Number: StrToFloat(fields[1]),
				}
				metrics = append(metrics, m)
			}
		}
	}

	m.prevCPUval = currCPUval
	m.prevCPUsum = currCPUsum

	return metrics, nil
}

func (m *Monitor) ProcMeminfo(content []byte) ([]mm.Metric, error) {
	m.logger.Debug("ProcMeminfo:call")
	defer m.logger.Debug("ProcMeminfo:return")

	m.status.Update(m.name, "Getting /proc/meminfo metrics")

	/**
	 * MemTotal:        8046892 kB
	 * MemFree:         5273644 kB
	 * Buffers:          300684 kB
	 * ...
	 */
	metrics := []mm.Metric{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 { // at least two fields expected
			continue
		}

		switch s := strings.TrimRight(fields[0], ":"); s {
		case "MemTotal", "MemFree", "MemShared", "Buffers", "Cached", "SwapCached", "SwapTotal", "SwapFree", "Dirty", "Active", "Inactive":
			m := mm.Metric{
				Name:   "memory/" + s,
				Type:   "gauge",
				Number: StrToFloat(fields[1]),
			}
			metrics = append(metrics, m)
		}
	}
	return metrics, nil
}

func (m *Monitor) ProcVmstat(content []byte) ([]mm.Metric, error) {
	m.logger.Debug("ProcVmstat:call")
	defer m.logger.Debug("ProcVmstat:return")

	m.status.Update(m.name, "Getting /proc/vmstat metrics")

	/**
	 * nr_free_pages 1318376
	 * nr_inactive_anon 1875
	 * nr_active_anon 322319
	 * ...
	 */
	metrics := []mm.Metric{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 { // at least two fields expected
			continue
		}

		if strings.HasPrefix(fields[0], "pswp") ||
			strings.HasPrefix(fields[0], "pgpg") ||
			strings.HasPrefix(fields[0], "numa") {
			m := mm.Metric{
				Name:   "vmstat/" + fields[0],
				Type:   "counter",
				Number: StrToFloat(fields[1]),
			}
			metrics = append(metrics, m)
		}
	}
	return metrics, nil
}

func (m *Monitor) ProcLoadavg(content []byte) ([]mm.Metric, error) {
	m.logger.Debug("ProcLoadavg:call")
	defer m.logger.Debug("ProcLoadavg:return")

	m.status.Update(m.name, "Getting /proc/loadavg metrics")

	/**
	 * Should be just one line:
	 * 0.00 0.01 0.05 1/201 16038
	 *
	 * "The first three fields in this file are load average figures
	 *  giving the number of jobs in the run queue (state R) or
	 *  waiting for disk I/O (state D) averaged over 1, 5, and 15
	 *  minutes.  They are the same as the load average numbers given
	 *  by uptime(1) and other programs.  The fourth field consists of
	 *  two numbers separated by a slash (/).  The first of these is
	 *  the number of currently runnable kernel scheduling entities
	 *  (processes, threads).  The value after the slash is the number
	 *  of kernel scheduling entities that currently exist on the
	 *  system.  The fifth field is the PID of the process that was
	 *  most recently created on the system."
	 * http://man7.org/linux/man-pages/man5/proc.5.html
	 */
	metrics := []mm.Metric{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 { // at least 4 fields expected
			continue
		}

		metrics = append(metrics, mm.Metric{Name: "loadavg/1min", Type: "gauge", Number: StrToFloat(fields[0])})
		metrics = append(metrics, mm.Metric{Name: "loadavg/5min", Type: "gauge", Number: StrToFloat(fields[1])})
		metrics = append(metrics, mm.Metric{Name: "loadavg/15min", Type: "gauge", Number: StrToFloat(fields[2])})

		procs := strings.Split(fields[3], "/")
		if len(procs) > 1 {
			metrics = append(metrics, mm.Metric{Name: "loadavg/running", Type: "gauge", Number: StrToFloat(procs[0])})
			metrics = append(metrics, mm.Metric{Name: "loadavg/processes", Type: "gauge", Number: StrToFloat(procs[1])})
		}

		break // stop after first valid line
	}

	return metrics, nil
}

func (m *Monitor) ProcDiskstats(content []byte) ([]mm.Metric, error) {
	m.logger.Debug("ProcDiskstats:call")
	defer m.logger.Debug("ProcDiskstats:return")

	m.status.Update(m.name, "Getting /proc/diskstats metrics")

	/**
	 *   1       0 ram0 0 0 0 0 0 0 0 0 0 0 0
	 *   1       1 ram1 0 0 0 0 0 0 0 0 0 0 0
	 * ...
	 *   8       0 sda 56058 2313 1270506 280760 232825 256917 10804063 2097320 0 1163068 2378728
	 *   8       1 sda1 385 1138 4518 4480 1 0 1 0 0 2808 4480
	 *   8       2 sda2 276 240 2104 1692 15 0 30 0 0 1592 1692
	 *   8       3 sda3 55223 932 1262468 270204 184397 256917 10804032 1436428 0 512824 1707280
	 *  11       0 sr0 0 0 0 0 0 0 0 0 0 0 0
	 * 252       0 dm-0 43661 0 1094074 262092 132099 0 5731328 4209168 0 231792 4471268
	 * ...
	 *
	 * Field 0: major device number
	 *       1: minor device number
	 *       2: device name
	 *    3-13: 11 stats: https://www.kernel.org/doc/Documentation/iostats.txt
	 */
	metrics := []mm.Metric{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 7 { // at least 7 fields expected
			continue
		}

		// Ignore ram and loop devices.
		device := fields[2]
		if strings.HasPrefix(device, "ram") || strings.HasPrefix(device, "loop") {
			continue
		}

		// 11 stats
		val := [14]float64{}
		for k := 3; k <= 13 && k < len(fields); k++ {
			val[k] = StrToFloat(fields[k])
		}

		if len(fields) > 10 {
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/reads", Type: "counter", Number: val[3]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/reads_merged", Type: "counter", Number: val[4]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/sectors_read", Type: "counter", Number: val[5]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/read_time", Type: "counter", Number: val[6]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/writes", Type: "counter", Number: val[7]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/writes_merged", Type: "counter", Number: val[8]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/sectors_written", Type: "counter", Number: val[9]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/write_time", Type: "counter", Number: val[10]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/io_time", Type: "counter", Number: val[12]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/io_time_weighted", Type: "counter", Number: val[13]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/iops", Type: "counter", Number: val[3] + val[7]})
		} else {
			// Early 2.6 kernels had only 4 fields for partitions.
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/reads", Type: "counter", Number: val[3]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/sectors_read", Type: "counter", Number: val[4]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/writes", Type: "counter", Number: val[5]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/sectors_written", Type: "counter", Number: val[6]})
			metrics = append(metrics, mm.Metric{Name: "disk/" + device + "/iops", Type: "counter", Number: val[3] + val[5]})
		}
	}
	return metrics, nil
}
