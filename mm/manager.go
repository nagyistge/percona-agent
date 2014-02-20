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

package mm

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/ticker"
	"time"
)

// We use one binding per unique Interval.Report.  For example, if some monitors
// report every 60s and others every 10s, then there are two bindings.  All monitors
// with the same report interval share the same binding: collectionChan to send
// metrics and aggregator summarizing and reporting those metrics when tickChan ticks.
type Binding struct {
	aggregator     *Aggregator
	collectionChan chan *Collection // <- metrics from monitors
	tickChan       chan time.Time   // -> aggregator reports
}

// todo: remember originating cmd for start service and start monitor,
//       return with ServiceIsRunningError

type Manager struct {
	logger   *pct.Logger
	monitors map[string]Monitor
	clock    ticker.Manager
	spool    data.Spooler
	// --
	config      *Config // nil if not running
	status      *pct.Status
	aggregators map[uint]*Binding
}

func NewManager(logger *pct.Logger, monitors map[string]Monitor, clock ticker.Manager, spool data.Spooler) *Manager {
	m := &Manager{
		logger:   logger,
		monitors: monitors,
		clock:    clock,
		spool:    spool,
		// --
		status:      pct.NewStatus([]string{"Mm"}),
		aggregators: make(map[uint]*Binding),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if m.IsRunning() {
		return pct.ServiceIsRunningError{"Mm"}
	}

	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.status.UpdateRe("Mm", "Starting", cmd)
	m.logger.Info("Starting", cmd)

	// We need one aggregator for each unique report interval.  There's usually
	// just one: 60s.  Remember: report interval != collect interval.  Monitors
	// can collect at different intervals (typically 1s and 10s), yet all report
	// at the same 60s interval, or different report intervals.
	for monitorName, interval := range c.Intervals {
		_, haveMonitor := m.monitors[monitorName]
		if !haveMonitor {
			return errors.New("Unknown monitor: " + monitorName)
		}

		if _, ok := m.aggregators[interval.Report]; !ok {
			logger := pct.NewLogger(m.logger.LogChan(), fmt.Sprintf("mm-ag-%d", interval.Report))

			tickChan := make(chan time.Time)
			m.clock.Add(tickChan, interval.Report)

			collectionChan := make(chan *Collection, 2*len(c.Intervals))
			aggregator := NewAggregator(logger, tickChan, collectionChan, m.spool)
			aggregator.Start()

			//msg := fmt.Sprintf("Synchronizing %d second report interval", interval.Report)
			//m.status.UpdateRe("Mm", msg, cmd)

			m.aggregators[interval.Report] = &Binding{aggregator, collectionChan, tickChan}
		}
	}

	m.config = c
	m.status.UpdateRe("Mm", "Ready", cmd)
	m.logger.Info("Ready", cmd)

	if err := pct.WriteConfig(CONFIG_FILE, c); err != nil {
		return err
	}

	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	// Stop all monitors.
	for name, monitor := range m.monitors {
		m.status.UpdateRe("Mm", "Stopping "+name, cmd)
		monitor.Stop()
	}

	// Stop and remove all aggregators.
	for n, b := range m.aggregators {
		b.aggregator.Stop()
		m.clock.Remove(b.tickChan)
		delete(m.aggregators, n)
	}

	m.config = nil
	m.status.UpdateRe("Mm", "Stopped", cmd)

	return nil
}

// @goroutine[0]
func (m *Manager) IsRunning() bool {
	if m.config != nil {
		return true
	}
	return false
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) error {
	defer m.status.Update("Mm", "Ready")

	// Agent should check IsRunning() and only call if true,
	// else return SerivceIsNotRunningError on our behalf.

	// Data contains name of sub-service (monitor) and its config.
	mm := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, mm); err != nil {
		return err
	}

	// Agent doesn't know which monitors we have; only we know.
	monitor, haveMonitor := m.monitors[mm.Name]
	if !haveMonitor {
		return errors.New("Unknown monitor: " + mm.Name)
	}

	// Start or stop the monitor.
	var err error
	switch cmd.Cmd {
	case "StartService":
		m.status.UpdateRe("Mm", "Starting "+mm.Name+" monitor", cmd)
		m.logger.Info("Start", mm.Name, "monitor", cmd)

		interval := m.config.Intervals[mm.Name]

		// When to collect.
		tickChan := make(chan time.Time)
		m.clock.Add(tickChan, interval.Collect)

		// Where to send metrics.
		a, ok := m.aggregators[interval.Report]
		if !ok {
			// Shouldn't happen.
			err = errors.New(fmt.Sprintf("No %ds aggregator for %s monitor report interval", interval.Report, mm.Name))
			break
		}

		// Run the Metrics Monitor!
		err = monitor.Start(mm.Config, tickChan, a.collectionChan)
	case "StopService":
		m.status.UpdateRe("Mm", "Stopping "+mm.Name+" monitor", cmd)
		m.logger.Info("Stop", mm.Name, "monitor", cmd)
		m.clock.Remove(monitor.TickChan())
		err = monitor.Stop()
	default:
		err = pct.UnknownCmdError{Cmd: cmd.Cmd}
	}

	if err != nil {
		m.logger.Warn(err)
	} else {
		m.logger.Info(cmd.Cmd, "OK")
	}

	return err
}

// @goroutine[1]
func (m *Manager) Status() map[string]string {
	return m.status.All()
}
