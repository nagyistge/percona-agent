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

package monitor

import (
	"fmt"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"sync"
	"time"
)

type MysqlInstance struct {
	logger      *pct.Logger
	mysqlConn   mysql.Connector
	Subscribers *Subscribers
	// --
	lastUptime      int64
	lastUptimeCheck time.Time
	sync.Mutex
}

func NewMysqlInstance(logger *pct.Logger, mysqlConn mysql.Connector, subscribers *Subscribers) (mi *MysqlInstance, err error) {
	if err := mysqlConn.Connect(2); err != nil {
		logger.Warn("Unable to connect to MySQL:", err)
		return nil, err
	}
	//defer mysqlConn.Close()

	// Get current MySQL uptime - this is later used to detect if MySQL was restarted
	lastUptime, err := mysqlConn.Uptime()
	if err != nil { // This shouldn't happen because we just opened the connection
		logger.Warn("Unespected connections close: ", err)
		return nil, err
	}
	lastUptimeCheck := time.Now()

	mi = &MysqlInstance{
		logger:          logger,
		mysqlConn:       mysqlConn,
		Subscribers:     subscribers,
		lastUptime:      lastUptime,
		lastUptimeCheck: lastUptimeCheck,
	}

	return mi, nil
}

func (m *MysqlInstance) CheckIfMysqlRestarted() bool {
	m.Lock()
	defer m.Unlock()

	if err := m.mysqlConn.Connect(1); err != nil {
		m.logger.Warn("Unable to connect to MySQL:", err)
		return false
	}
	defer m.mysqlConn.Close()

	lastUptime := m.lastUptime
	lastUptimeCheck := m.lastUptimeCheck
	currentUptime, err := m.mysqlConn.Uptime()
	if err != nil {
		m.logger.Error(err)
		return false
	}

	m.logger.Debug(fmt.Sprintf("lastUptime=%d lastUptimeCheck=%s currentUptime=%d",
		lastUptime, lastUptimeCheck.UTC(), currentUptime))

	// Calculate expected uptime
	//   This protects against situation where after restarting MySQL
	//   we are unable to connect to it for period longer than last registered uptime
	//
	// Steps to reproduce:
	// * currentUptime=60 lastUptime=0
	// * Restart MySQL
	// * QAN connection problem for 120s
	// * currentUptime=120 lastUptime=60 (new uptime (120s) is higher than last registered (60s))
	// * elapsedTime=120s (time elapsed since last check)
	// * expectedUptime= 60s + 120s = 180s
	// * 120s < 180s (currentUptime < expectedUptime) => server was restarted
	elapsedTime := time.Now().Unix() - lastUptimeCheck.Unix()
	expectedUptime := lastUptime + elapsedTime
	m.logger.Debug(fmt.Sprintf("elapsedTime=%d expectedUptime=%d", elapsedTime, expectedUptime))

	// Save uptime from last check
	m.lastUptime = currentUptime
	m.lastUptimeCheck = time.Now()

	// If current server uptime is lower than last registered uptime
	// then we can assume that server was restarted
	if currentUptime < expectedUptime {
		return true
	}

	return false
}

func (m *MysqlInstance) DSN() string {
	return m.mysqlConn.DSN()
}
