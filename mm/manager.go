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
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/ticker"
	"time"
)

// We use one binding per unique mm.Report interval.  For example, if some monitors
// report every 60s and others every 10s, then there are two bindings.  All monitors
// with the same report interval share the same binding: collectionChan to send
// metrics and aggregator summarizing and reporting those metrics.
type Binding struct {
	aggregator     *Aggregator
	collectionChan chan *Collection // <- metrics from monitors
}

// todo: remember originating cmd for start service and start monitor,
//       return with ServiceIsRunningError

type Manager struct {
	logger  *pct.Logger
	factory MonitorFactory
	clock   ticker.Manager
	spool   data.Spooler
	im      *instance.Repo
	// --
	monitors    map[string]Monitor
	status      *pct.Status
	aggregators map[uint]*Binding
	configDir   string
}

func NewManager(logger *pct.Logger, factory MonitorFactory, clock ticker.Manager, spool data.Spooler, im *instance.Repo) *Manager {
	m := &Manager{
		logger:  logger,
		factory: factory,
		clock:   clock,
		spool:   spool,
		im:      im,
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
	defer m.status.Update("mm", "Ready")

	switch cmd.Cmd {
	case "StartService":
		mm, name, err := m.getMonitorConfig(cmd)
		if err != nil {
			return cmd.Reply(nil, err)
		}

		m.status.UpdateRe("mm", "Starting "+name, cmd)
		m.logger.Info("Start", name, cmd)

		// Monitors names must be unique.
		_, haveMonitor := m.monitors[name]
		if haveMonitor {
			return cmd.Reply(nil, errors.New("Duplicate monitor: "+name))
		}

		// Create the monitor based on its type.
		monitor, err := m.factory.Make(mm.Service, mm.InstanceId, cmd.Data)
		if err != nil {
			return cmd.Reply(nil, errors.New("Factory: "+err.Error()))
		}

		// Make synchronized (3rd arg=true) ticker for collect interval.  It's
		// synchronized so all data aligns in charts, else we can get MySQL metrics
		// at 00:03 and system metrics at 00:05 and other metrics at 00:06 which
		// makes it very difficult to see all metrics at a single point in time
		// or meaningfully compare a single interval, e.g. 00:00 to 00:05.
		tickChan := make(chan time.Time)
		m.clock.Add(tickChan, mm.Collect, true)

		// We need one aggregator for each unique report interval.  There's usually
		// just one: 60s.  Remember: report interval != collect interval.  Monitors
		// can collect at different intervals (typically 1s and 10s), yet all report
		// at the same 60s interval, or different report intervals.
		a, ok := m.aggregators[mm.Report]
		if !ok {
			// Make new aggregator for this report interval.
			logger := pct.NewLogger(m.logger.LogChan(), fmt.Sprintf("mm-ag-%d", mm.Report))
			collectionChan := make(chan *Collection, 2*len(m.monitors)+1)
			aggregator := NewAggregator(logger, int64(mm.Report), collectionChan, m.spool)
			aggregator.Start()

			// Save aggregator for other monitors with same report interval.
			a = &Binding{aggregator, collectionChan}
			m.aggregators[mm.Report] = a
			m.logger.Info("Created", mm.Report, "second aggregator")
		}

		// Start the monitor.
		if err := monitor.Start(tickChan, a.collectionChan); err != nil {
			return cmd.Reply(nil, errors.New("Start "+name+": "+err.Error()))
		}
		m.monitors[name] = monitor

		// Save the monitor-specific config to disk so agent starts on restart.
		monitorConfig := monitor.Config()
		if err := pct.Basedir.WriteConfig(name, monitorConfig); err != nil {
			return cmd.Reply(nil, errors.New("Write "+name+" config:"+err.Error()))
		}

		return cmd.Reply(nil) // success
	case "StopService":
		_, name, err := m.getMonitorConfig(cmd)
		if err != nil {
			return cmd.Reply(nil, err)
		}
		m.status.UpdateRe("mm", "Stopping "+name, cmd)
		m.logger.Info("Stop", name, cmd)
		monitor, ok := m.monitors[name]
		if !ok {
			return cmd.Reply(nil, errors.New("Unknown monitor: "+name))
		}
		if err := monitor.Stop(); err != nil {
			return cmd.Reply(nil, errors.New("Stop "+name+": "+err.Error()))
		}
		m.clock.Remove(monitor.TickChan())
		if err := pct.Basedir.RemoveConfig(name); err != nil {
			return cmd.Reply(nil, errors.New("Remove "+name+": "+err.Error()))
		}
		delete(m.monitors, name)
		return cmd.Reply(nil) // success
	case "GetConfig":
		configs := make(map[string]string)
		for name, monitor := range m.monitors {
			config := monitor.Config()
			bytes, err := json.Marshal(config)
			if err != nil {
				m.logger.Error(err)
			}
			configs[name] = string(bytes)
		}
		return cmd.Reply(configs)
	default:
		// SetConfig does not work by design.  To re-configure a monitor,
		// stop it then start it again with the new config.
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) LoadConfig(configDir string) ([]byte, error) {
	// mm is a proxy manager so it doesn't have its own config.  To get a monitor config:
	// [Service:mm Cmd:GetConfig: Data:mm.Config[Name:..., Type:...]]
	return nil, nil
}

// @goroutine[1]
func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) getMonitorConfig(cmd *proto.Cmd) (*Config, string, error) {
	/**
	 * cmd.Data is a monitor-specific config, e.g. mysql.Config.  But monitor-specific
	 * configs embed mm.Config, so get that first to determine the monitor's name and
	 * type which is all we need to start it.  The monitor itself will decode cmd.Data
	 * into it's specific config, which we fetch back later by calling monitor.Config()
	 * to save to disk.
	 */
	mm := &Config{}
	if cmd.Data != nil {
		if err := json.Unmarshal(cmd.Data, mm); err != nil {
			return nil, "", errors.New("mm.getMonitorConfig:json.Unmarshal:" + err.Error())
		}
	}

	// The real name of the internal service, e.g. mm-mysql-1:
	name := "mm-" + m.im.Name(mm.Service, mm.InstanceId)

	return mm, name, nil
}
