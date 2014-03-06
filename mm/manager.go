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
	"strings"
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
	configDir   string
}

func NewManager(logger *pct.Logger, factory MonitorFactory, clock ticker.Manager, spool data.Spooler) *Manager {
	m := &Manager{
		logger:  logger,
		factory: factory,
		clock:   clock,
		spool:   spool,
		// --
		monitors:    make(map[string]Monitor),
		status:      pct.NewStatus([]string{"mm"}),
		aggregators: make(map[uint]*Binding),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	m.status.Update("mm", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.Update("mm", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("mm", "Handling", cmd)
	var err error

	defer func() {
		if err != nil {
			m.logger.Error(err)
		}
		m.status.Update("mm", "Ready")
	}()

	mm := &Config{}
	if err = json.Unmarshal(cmd.Data, mm); err != nil {
		return cmd.Reply(nil, errors.New("JSON: " + err.Error()))
	}

	// e.g. default-mysql-monitor, default-system-monitoir,
	// db1-msyql-monitor, db2-mysql-monitor
	name := strings.ToLower(mm.Name + "-" + mm.Type + "-monitor")

	switch cmd.Cmd {
	case "StartService":
		m.status.UpdateRe("mm", "Starting "+name, cmd)
		m.logger.Info("Start", name, cmd)

		// Monitors names must be unique.
		_, haveMonitor := m.monitors[name]
		if haveMonitor {
			return cmd.Reply(nil, errors.New("Duplicate monitor: "+name))
		}

		// Create the monitor based on its type.
		var monitor Monitor
		if monitor, err = m.factory.Make(mm.Type, name); err != nil {
			return cmd.Reply(nil, errors.New("Factory: " + err.Error()))
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
			return cmd.Reply(nil, errors.New("Start " + name + ": " + err.Error()))
		}
		m.monitors[name] = monitor

		// Save the monitor config to disk so agent starts on restart.
		if err = m.WriteConfig(mm, name); err != nil {
			return cmd.Reply(nil, errors.New("Write " + name + " config:" + err.Error()))
		}
	case "StopService":
		m.status.UpdateRe("mm", "Stopping "+name, cmd)
		m.logger.Info("Stop", name, cmd)
		if monitor, ok := m.monitors[name]; ok {
			m.clock.Remove(monitor.TickChan())
			if err = monitor.Stop(); err != nil {
				return cmd.Reply(nil, errors.New("Stop " + name + ": " + err.Error()))
			}
			if err := m.RemoveConfig(name); err != nil {
				return cmd.Reply(nil, errors.New("Remove " + name + ": " + err.Error()))
			}
		} else {
			return cmd.Reply(nil, errors.New("Unknown monitor: "+name))
		}
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}

	return cmd.Reply(nil) // success
}

func (m *Manager) LoadConfig(configDir string) ([]byte, error) {
	// mm is a proxy manager so it doesn't have its own config.  To get a monitor config:
	// [Service:mm Cmd:GetConfig: Data:mm.Config[Name:..., Type:...]]
	m.configDir = configDir
	return nil, nil
}

func (m *Manager) WriteConfig(config interface{}, name string) error {
	// Write a monitor config.
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + name + ".conf"
	m.logger.Info("Writing", file)
	return pct.WriteConfig(file, config)
}

func (m *Manager) RemoveConfig(name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + name + ".conf"
	m.logger.Info("Removing", file)
	return pct.RemoveFile(file)
}

// @goroutine[1]
func (m *Manager) Status() map[string]string {
	return m.status.All()
}
