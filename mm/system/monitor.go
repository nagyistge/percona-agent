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
	"encoding/json"
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
	logger *pct.Logger
	// --
	config         *Config
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
	// --
	prevCPUval map[string][]float64 // [cpu0] => [user, nice, ...]
	prevCPUsum map[string]float64   // [cpu0] => user + nice + ...
	sync       *pct.SyncChan
	status     *pct.Status
}

func NewMonitor(logger *pct.Logger) *Monitor {
	m := &Monitor{
		logger: logger,
		// --
		prevCPUval: make(map[string][]float64),
		prevCPUsum: make(map[string]float64),
		status:     pct.NewStatus([]string{"system-monitor"}),
		sync:       pct.NewSyncChan(),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(config []byte, tickChan chan time.Time, collectionChan chan *mm.Collection) error {
	if m.config != nil {
		return pct.ServiceIsRunningError{"system-monitor"}
	}

	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.config = c
	m.tickChan = tickChan
	m.collectionChan = collectionChan

	go m.run()

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	if m.config == nil {
		return nil // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update("system-monitor", "Stopping")
	m.sync.Stop()
	m.sync.Wait()

	m.config = nil // no config if not running

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
	defer func() {
		m.status.Update("system-monitor", "Stopped")
		m.sync.Done()
	}()

	for {
		m.status.Update("system-monitor", "Idle")

		select {
		case now := <-m.tickChan:
			m.status.Update("system-monitor", "Running")

			c := &mm.Collection{
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
				case <-time.After(500 * time.Millisecond):
					// lost collection
					m.logger.Debug("Lost system metrics; timeout spooling after 500ms")
				}
			} else {
				m.logger.Debug("No metrics") // shouldn't happen
			}

			m.status.Update("system-monitor", "Ready")
		case <-m.sync.StopChan:
			return
		}
	}
}

func (m *Monitor) ProcStat(content []byte) ([]mm.Metric, error) {
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
	metrics := []mm.Metric{}
	/*
		lines = strings.Split(string(content), "\n")
		for _, v := range lines {
			fields := strings.Fields(v)
			if len(fields) < 4 { // at least 4 fields expected
				continue
			}

			storage[metrics.GetIdByName("loadavg/1min")] = metrics.NewMetricValue(StrToFloat(fields[0]))
			storage[metrics.GetIdByName("loadavg/5min")] = metrics.NewMetricValue(StrToFloat(fields[1]))
			storage[metrics.GetIdByName("loadavg/15min")] = metrics.NewMetricValue(StrToFloat(fields[2]))
			procs := strings.Split(fields[3], "/")
			if len(procs) > 1 {
				storage[metrics.GetIdByName("loadavg/running")] = metrics.NewMetricValue(StrToFloat(procs[0]))
				storage[metrics.GetIdByName("loadavg/processes")] = metrics.NewMetricValue(StrToFloat(procs[1]))
			}
			break
		}
	*/
	return metrics, nil
}

func (m *Monitor) ProcDiskstats(content []byte) ([]mm.Metric, error) {
	metrics := []mm.Metric{}
	/*
		lines = strings.Split(string(content), "\n")
		for _, v := range lines {
			fields := strings.Fields(v)
			if len(fields) < 7 { // at least 7 fields expected
				continue
			}
			// we ignore ram and loop devices
			if strings.HasPrefix(fields[2], "ram") {
				continue
			}
			if strings.HasPrefix(fields[2], "loop") {
				continue
			}

			var currVals [14]metrics.MetricType


			for k := 3; k <= 13 && k < len(fields); k++ {
				currVals[k] = StrToFloat(fields[k])
			}

			if len(fields) > 10 {
				storage[metrics.GetIdByName("disk/"+fields[2]+"/reads")] = metrics.NewMetricCounter(currVals[3])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/reads_merged")] = metrics.NewMetricCounter(currVals[4])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/sectors_read")] = metrics.NewMetricCounter(currVals[5])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/read_time")] = metrics.NewMetricCounter(currVals[6])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/writes")] = metrics.NewMetricCounter(currVals[7])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/writes_merged")] = metrics.NewMetricCounter(currVals[8])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/sectors_written")] = metrics.NewMetricCounter(currVals[9])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/write_time")] = metrics.NewMetricCounter(currVals[10])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/io_time")] = metrics.NewMetricCounter(currVals[12])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/io_time_weighted")] = metrics.NewMetricCounter(currVals[13])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/iops")] = metrics.NewMetricCounter(currVals[3] + currVals[7])
			} else { // Early 2.6 kernels had only 4 fields for partitions.
				storage[metrics.GetIdByName("disk/"+fields[2]+"/reads")] = metrics.NewMetricCounter(currVals[3])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/sectors_read")] = metrics.NewMetricCounter(currVals[4])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/writes")] = metrics.NewMetricCounter(currVals[5])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/sectors_written")] = metrics.NewMetricCounter(currVals[6])
				storage[metrics.GetIdByName("disk/"+fields[2]+"/iops")] = metrics.NewMetricCounter(currVals[3] + currVals[5])
			}
		}
	*/
	return metrics, nil
}
