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
	"encoding/json"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sysconfig"
	"strings"
	"time"
)

type Monitor struct {
	logger *pct.Logger
	// --
	config        *Config
	tickChan      chan time.Time
	sysconfigChan chan *sysconfig.SystemConfig
	// --
	status *pct.Status
	sync   *pct.SyncChan
}

func NewMonitor(logger *pct.Logger) *Monitor {
	m := &Monitor{
		logger: logger,
		// --
		sync:   pct.NewSyncChan(),
		status: pct.NewStatus([]string{"mysql-sysconfig"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(config []byte, tickChan chan time.Time, sysconfigChan chan *sysconfig.SystemConfig) error {
	if m.config != nil {
		return pct.ServiceIsRunningError{"mysql-monitor"}
	}

	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return errors.New("mysql.Start:json.Unmarshal:" + err.Error())
	}

	m.config = c
	m.tickChan = tickChan
	m.sysconfigChan = sysconfigChan

	go m.run()

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	if m.config == nil {
		return nil // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update("mysql-sysconfig", "Stopping")
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

// @goroutine[1]
func (m *Monitor) connect() (*sql.DB, error) {
	m.logger.Debug("Connecting")
	m.status.Update("mysql-sysconfig", "Connecting")

	// Open connection to MySQL but...
	m.logger.Debug("DSN:", m.config.DSN)
	db, err := sql.Open("mysql", m.config.DSN)
	if err != nil {
		m.logger.Error("sql.Open: ", err)
		return nil, err
	}

	// ...try to use the connection for real.
	if err := db.Ping(); err != nil {
		// Connection failed.  Wrong username or password?
		m.logger.Warn("db.Ping: ", err)
		db.Close()
		return nil, err
	}

	return db, nil
}

// @goroutine[2]
func (m *Monitor) run() {
	defer func() {
		m.status.Update("mysql-sysconfig", "Stopped")
		m.sync.Done()
	}()

	prefix := m.config.InstanceName

	for {
		m.logger.Debug("Ready")
		m.status.Update("mysql-sysconfig", "Ready")

		select {
		case now := <-m.tickChan:
			conn, err := m.connect()
			if err != nil {
				m.logger.Warn(err)
				continue
			}

			m.logger.Debug("Running")
			m.status.Update("mysql-sysconfig", "Running")
			c := &sysconfig.SystemConfig{
				Ts:     now.UTC().Unix(),
				System: "mysql global variables",
				Config: []sysconfig.Setting{},
			}
			if err := m.GetGlobalVariables(conn, prefix, c); err != nil {
				m.logger.Warn(err)
			}
			conn.Close()

			if len(c.Config) > 0 {
				select {
				case m.sysconfigChan <- c:
				case <-time.After(500 * time.Millisecond):
					// lost sysconfig
					m.logger.Debug("Lost MySQL settings; timeout spooling after 500ms")
				}
			} else {
				m.logger.Debug("No settings") // shouldn't happen
			}
		case <-m.sync.StopChan:
			return
		}
	}
}

// @goroutine[2]
func (m *Monitor) GetGlobalVariables(conn *sql.DB, prefix string, c *sysconfig.SystemConfig) error {
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

		varName = prefix + "/" + strings.ToLower(varName)

		c.Config = append(c.Config, sysconfig.Setting{varName, varValue})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}
