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

/**
 * mm is a proxy manager for monitors.  It implements the service manager
 * interface (pct/service.go), but it's always running.  Its main job is
 * done in Handle(): keeping track of the monitors it starts and stops.
 */

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
	logger  *pct.Logger
	factory MonitorFactory
	clock   ticker.Manager
	spool   data.Spooler
	// --
	monitors    map[string]Monitor
	status      *pct.Status
	aggregators map[uint]*Binding
}

func NewManager(logger *pct.Logger, factory MonitorFactory, clock ticker.Manager, spool data.Spooler) *Manager {
	m := &Manager{
		logger:  logger,
		factory: factory,
		clock:   clock,
		spool:   spool,
		// --
		monitors:    make(map[string]Monitor),
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
	m.status.Update("Mm", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.Update("Mm", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	defer m.status.Update("Mm", "Ready")

	mm := &Config{}
	if err := json.Unmarshal(cmd.Data, mm); err != nil {
		return err
	}

	// Start or stop the
	var err error
	switch cmd.Cmd {
	case "StartService":
		m.status.UpdateRe("Mm", "Starting "+mm.Name+" monitor", cmd)
		m.logger.Info("Start", mm.Name, "monitor", cmd)

		// Monitors names must be unique.
		_, haveMonitor := m.monitors[mm.Name]
		if haveMonitor {
			return errors.New("Duplicate monitor: " + mm.Name)
		}

		// Create the monitor based on its type.
		var monitor Monitor
		if monitor, err = m.factory.Make(mm.Type, mm.Name); err != nil {
			return err
		}

		// Make ticker for collect interval.
		tickChan := make(chan time.Time)
		m.clock.Add(tickChan, mm.Collect)

		// We need one aggregator for each unique report interval.  There's usually
		// just one: 60s.  Remember: report interval != collect interval.  Monitors
		// can collect at different intervals (typically 1s and 10s), yet all report
		// at the same 60s interval, or different report intervals.
		a, ok := m.aggregators[mm.Report]
		if !ok {
			// Make new aggregator for this report interval.
			logger := pct.NewLogger(m.logger.LogChan(), fmt.Sprintf("mm-ag-%d", mm.Report))
			tickChan := make(chan time.Time)
			m.clock.Add(tickChan, mm.Report)
			collectionChan := make(chan *Collection, 2*len(m.monitors)+1)
			aggregator := NewAggregator(logger, tickChan, collectionChan, m.spool)
			aggregator.Start()

			// Save aggregator for other monitors with same report interval.
			a = &Binding{aggregator, collectionChan, tickChan}
			m.aggregators[mm.Report] = a
			m.logger.Info("Created", mm.Report, "second aggregator")
		}

		// Start the monitor.
		if err = monitor.Start(mm.Config, tickChan, a.collectionChan); err != nil {
			return err
		}
		m.monitors[mm.Name] = monitor

		// Save the monitor config to disk so agent starts on restart.
		if err := pct.WriteConfig(mm.Name+"-monitor.conf", mm); err != nil {
			return err
		}
	case "StopService":
		m.status.UpdateRe("Mm", "Stopping "+mm.Name+" monitor", cmd)
		m.logger.Info("Stop", mm.Name, "monitor", cmd)
		if monitor, ok := m.monitors[mm.Name]; ok {
			m.clock.Remove(monitor.TickChan())
			err = monitor.Stop()
		} else {
			return errors.New("Unknown monitor: " + mm.Name)
		}
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
func (m *Manager) Status() string {
	return m.status.Get("Mm", true)
}
