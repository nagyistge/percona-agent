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

package mrms

import (
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"sync"
	"time"
)

type Manager struct {
	logger           *pct.Logger
	mysqlConnFactory mysql.ConnectionFactory
	mysqlInstances   map[string]*mysqlInstance
	sync.RWMutex
}

type mysqlInstance struct {
	logger          *pct.Logger
	mysqlConn       mysql.Connector
	lastUptime      int64
	lastUptimeCheck time.Time
	subscribers     map[chan bool]bool
	sync.RWMutex
}

func (m *mysqlInstance) Add() (c chan bool) {
	m.Lock()
	defer m.Unlock()

	c = make(chan bool, 5)
	m.subscribers[c] = true

	return c
}

func (m *mysqlInstance) Remove(c chan bool) {
	m.Lock()
	defer m.Unlock()

	if m.subscribers[c] {
		delete(m.subscribers, c)
	}
}

func (m *mysqlInstance) Empty() bool {
	m.RLock()
	defer m.RUnlock()

	return len(m.subscribers) == 0
}

func (m *mysqlInstance) CheckIfMysqlRestarted() bool {
	lastUptime := m.lastUptime
	lastUptimeCheck := m.lastUptimeCheck
	currentUptime := m.mysqlConn.Uptime()

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

func (m *mysqlInstance) NotifySubscribers() {
	m.RLock()
	defer m.RUnlock()

	for c, _ := range m.subscribers {
		select {
		case c <- true:
		default:
			m.logger.Warn("Unable to notify subscriber")
		}
	}
}

func NewManager(logger *pct.Logger, mysqlConnFactory mysql.ConnectionFactory) MRMS {
	m := &Manager{
		logger:           logger,
		mysqlConnFactory: mysqlConnFactory,
		mysqlInstances:   make(map[string]*mysqlInstance),
	}
	return m
}

func (m *Manager) Add(dsn string) (c chan bool, err error) {
	m.Lock()
	defer m.Unlock()

	mysqlInstance, ok := m.mysqlInstances[dsn]
	if !ok {
		mysqlInstance, err = m.createMysqlInstance(dsn)
		if err != nil {
			return nil, err
		}

		m.mysqlInstances[dsn] = mysqlInstance
	}

	c = mysqlInstance.Add()

	return c, nil
}

func (m *Manager) Remove(dsn string, c chan bool) {
	m.Lock()
	defer m.Unlock()

	if mysqlInstance, ok := m.mysqlInstances[dsn]; ok {
		mysqlInstance.Remove(c)
		if mysqlInstance.Empty() {
			delete(m.mysqlInstances, dsn)
		}
	}
}

func (m *Manager) Run() {
	m.RLock()
	defer m.RUnlock()

	for _, mysqlInstance := range m.mysqlInstances {
		if mysqlInstance.CheckIfMysqlRestarted() {
			mysqlInstance.NotifySubscribers()
		}
	}
}

func (m *Manager) createMysqlInstance(dsn string) (mi *mysqlInstance, err error) {
	mysqlConn := m.mysqlConnFactory.Make(dsn)
	if err := mysqlConn.Connect(2); err != nil {
		m.logger.Warn("Unable to connect to MySQL: %s", err)
		return nil, err
	}
	defer mysqlConn.Close()

	// Get current MySQL uptime - this is later used to detect if MySQL was restarted
	lastUptime := mysqlConn.Uptime()
	lastUptimeCheck := time.Now()

	mi = &mysqlInstance{
		logger:          m.logger,
		mysqlConn:       mysqlConn,
		lastUptime:      lastUptime,
		lastUptimeCheck: lastUptimeCheck,
		subscribers:     make(map[chan bool]bool),
	}

	return mi, nil
}
