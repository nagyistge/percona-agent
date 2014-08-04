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
	"database/sql"
	"fmt"
	_ "github.com/arnehormann/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/sysconfig"
	"strings"
	"time"
)

type Monitor struct {
	name   string
	config *Config
	logger *pct.Logger
	conn   mysql.Connector
	// --
	tickChan   chan time.Time
	reportChan chan *sysconfig.Report
	status     *pct.Status
	sync       *pct.SyncChan
	running    bool
}

func NewMonitor(name string, config *Config, logger *pct.Logger, conn mysql.Connector) *Monitor {
	m := &Monitor{
		name:   name,
		config: config,
		logger: logger,
		conn:   conn,
		// --
		sync:   pct.NewSyncChan(),
		status: pct.NewStatus([]string{name, name + "-mysql"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(tickChan chan time.Time, reportChan chan *sysconfig.Report) error {
	if m.running {
		return pct.ServiceIsRunningError{m.name}
	}

	m.status.Update(m.name, "Starting")
	m.tickChan = tickChan
	m.reportChan = reportChan
	go m.run()
	m.running = true
	m.logger.Info("Started")
	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	if !m.running {
		return nil // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update(m.name, "Stopping")
	m.sync.Stop()
	m.sync.Wait()
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

// @goroutine[2]
func (m *Monitor) run() {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("Recovered while running sysconfig monitor: ", r)
		}
		m.status.Update(m.name, "Stopped")
		m.sync.Done()
	}()

	var lastTs int64
	for {
		m.logger.Debug("run:idle")
		m.status.Update(m.name, fmt.Sprintf("Idle (last collected at %s)", time.Unix(lastTs, 0)))

		select {
		case now := <-m.tickChan:
			m.logger.Debug("run:collect:start")
			m.status.Update(m.name, "Running")

			// Connect to MySQL.
			m.status.Update(m.name+"-mysql", "Connecting")
			if err := m.conn.Connect(2); err != nil {
				m.logger.Warn(err)
				m.status.Update(m.name+"-mysql", "Error: "+err.Error())
				continue
			}
			m.status.Update(m.name+"-mysql", "Connected")

			c := &sysconfig.Report{
				ServiceInstance: proto.ServiceInstance{
					Service:    m.config.Service,
					InstanceId: m.config.InstanceId,
				},
				Ts:       now.UTC().Unix(),
				System:   "mysql global variables",
				Settings: []sysconfig.Setting{},
			}

			// Get SHOW GLOBAL VARIABLES.
			if err := m.GetGlobalVariables(m.conn.DB(), c); err != nil {
				m.logger.Warn(err)
			}

			// Disconnect from MySQL.
			m.conn.Close()
			m.status.Update(m.name+"-mysql", "Disconnected (OK)")

			if len(c.Settings) > 0 {
				select {
				case m.reportChan <- c:
					lastTs = c.Ts
				case <-time.After(500 * time.Millisecond):
					// lost sysconfig
					m.logger.Debug("Lost MySQL settings; timeout spooling after 500ms")
				}
			} else {
				m.logger.Debug("No settings") // shouldn't happen
			}

			m.logger.Debug("run:collect:stop")
		case <-m.sync.StopChan:
			m.logger.Debug("run:stop")
			return
		}
	}
}

// @goroutine[2]
func (m *Monitor) GetGlobalVariables(conn *sql.DB, c *sysconfig.Report) error {
	m.logger.Debug("Getting global variables")
	m.status.Update(m.name, "Getting global variables")

	m.status.Update(m.name, "Getting SHOW GLOBAL VARIABLES")

	rows, err := conn.Query("SHOW /*!50002 GLOBAL */ VARIABLES")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var varName string
		var varValue string
		if err = rows.Scan(&varName, &varValue); err != nil {
			return err
		}
		varName = strings.ToLower(varName)
		c.Settings = append(c.Settings, sysconfig.Setting{varName, varValue})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}
