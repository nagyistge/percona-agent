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

package sysconfig

/**
 * sysconfig is a proxy manager for monitors.  It implements the service manager
 * interface (pct/service.go), but it's always running.  Its main job is done in
 * Handle(): keeping track of the monitors it starts and stops.
 */

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/ticker"
	"strings"
	"time"
)

type Manager struct {
	logger  *pct.Logger
	factory MonitorFactory
	clock   ticker.Manager
	spool   data.Spooler
	// --
	sysconfigChan  chan *SystemConfig // <- SystemConfig from monitor
	monitors       map[string]Monitor
	status         *pct.Status
	configDir      string
	spoolerRunning bool
}

func NewManager(logger *pct.Logger, factory MonitorFactory, clock ticker.Manager, spool data.Spooler) *Manager {
	m := &Manager{
		logger:  logger,
		factory: factory,
		clock:   clock,
		spool:   spool,
		// --
		sysconfigChan: make(chan *SystemConfig, 3),
		monitors:      make(map[string]Monitor),
		status:        pct.NewStatus([]string{"sysconfig", "sysconfig-spooler"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if !m.spoolerRunning {
		go m.spooler()
	}
	m.status.Update("sysconfig", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.Update("sysconfig", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("sysconfig", "Handling", cmd)
	var err error

	defer func() {
		if err != nil {
			m.logger.Error(err)
		}
		m.status.Update("sysconfig", "Ready")
	}()

	/**
	 * cmd.Data is a monitor-specific config, e.g. mysql.Config.  But monitor-specific
	 * configs embed c.Config, so get that first to determine the monitor's name and
	 * type which is all we need to start it.  The monitor itself will decode cmd.Data
	 * into it's specific config, which we fetch back later by calling monitor.Config()
	 * to save to disk.
	 */
	c := &Config{}
	if err = json.Unmarshal(cmd.Data, c); err != nil {
		return cmd.Reply(nil, errors.New("sysconfig.Handle:json.Unmarshal:"+err.Error()))
	}

	name := strings.ToLower("sysconfig-" + c.Type + "-" + c.Name)

	switch cmd.Cmd {
	case "StartService":
		m.status.UpdateRe("sysconfig", "Starting "+name, cmd)
		m.logger.Info("Start", name, cmd)

		// Monitors names must be unique.
		_, haveMonitor := m.monitors[name]
		if haveMonitor {
			return cmd.Reply(nil, errors.New("Duplicate monitor: "+name))
		}

		// Create the monitor based on its type.
		var monitor Monitor
		if monitor, err = m.factory.Make(c.Type, name); err != nil {
			return cmd.Reply(nil, errors.New("Factory: "+err.Error()))
		}

		// Make ticker for collect interval.
		tickChan := make(chan time.Time)
		m.clock.Add(tickChan, c.Collect)

		// Start the monitor.
		if err = monitor.Start(cmd.Data, tickChan, m.sysconfigChan); err != nil {
			return cmd.Reply(nil, errors.New("Start "+name+": "+err.Error()))
		}
		m.monitors[name] = monitor

		// Save the monitor-specific config to disk so agent starts on restart.
		monitorConfig := monitor.Config()
		if err = m.WriteConfig(monitorConfig, name); err != nil {
			return cmd.Reply(nil, errors.New("Write "+name+" config:"+err.Error()))
		}
	case "StopService":
		m.status.UpdateRe("sysconfig", "Stopping "+name, cmd)
		m.logger.Info("Stop", name, cmd)
		if monitor, ok := m.monitors[name]; ok {
			m.clock.Remove(monitor.TickChan())
			if err = monitor.Stop(); err != nil {
				return cmd.Reply(nil, errors.New("Stop "+name+": "+err.Error()))
			}
			if err := m.RemoveConfig(name); err != nil {
				return cmd.Reply(nil, errors.New("Remove "+name+": "+err.Error()))
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

// --------------------------------------------------------------------------

func (m *Manager) spooler() {
	defer m.status.Update("sysconfig-spooler", "Stopped")
	m.status.Update("sysconfig-spooler", "Running")
	for s := range m.sysconfigChan {
		m.spool.Write("sysconfig", s)
	}
}
