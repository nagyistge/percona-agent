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

package mysql

import (
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
)

const (
	MONITOR_NAME = "mysql"
)

type Monitor struct {
	name   string
	config *Config
	logger *pct.Logger
	conn   mysql.Connector
	// --
	status  *pct.Status
	running bool
}

func NewMonitor(name string, config *Config, logger *pct.Logger, conn mysql.Connector) *Monitor {
	m := &Monitor{
		name:   name,
		config: config,
		logger: logger,
		conn:   conn,
		// --
		status: pct.NewStatus([]string{name, fmt.Sprintf("%s-%s", name, MONITOR_NAME)}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start() error {
	m.logger.Debug("Start:call")
	defer m.logger.Debug("Start:return")

	if m.running {
		return pct.ServiceIsRunningError{m.name}
	}

	m.running = true
	m.logger.Info("Started")
	m.status.Update(m.name, "Ready")

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	m.logger.Debug("Stop:call")
	defer m.logger.Debug("Stop:return")

	if !m.running {
		return nil // already stopped
	}

	m.config = nil // no config if not running
	m.running = false

	m.logger.Info("Stopped")
	m.status.Update(m.name, "Stopped")
	return nil
}

// @goroutine[0]
func (m *Monitor) Status() map[string]string {
	return m.status.All()
}

// @goroutine[0]
func (m *Monitor) Config() interface{} {
	return m.config
}

func (m *Monitor) Explain(query string) (explain *mysql.Explain, err error) {
	if err = m.conn.Connect(2); err != nil {
		panic("a")
		return nil, err
	}
	if explain, err = m.conn.Explain(query); err != nil {
		panic("b")
		return nil, err
	}

	return explain, nil
}
