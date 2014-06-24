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
	"github.com/percona/percona-agent/mrms"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"sync"
	"time"
)

type Monitor struct {
	logger           *pct.Logger
	mysqlConnFactory mysql.ConnectionFactory
	// --
	mysqlInstances map[string]*mysqlInstance
	sync.RWMutex
	// --
	stop chan bool
}

func NewMonitor(logger *pct.Logger, mysqlConnFactory mysql.ConnectionFactory) mrms.Monitor {
	m := &Monitor{
		logger:           logger,
		mysqlConnFactory: mysqlConnFactory,
		mysqlInstances:   make(map[string]*mysqlInstance),
		stop:             make(chan bool, 1),
	}
	return m
}

func (m *Monitor) Start() error {
	go func() {
		m.Run() // Immediately run first check
		for {
			sleep := time.After(1 * time.Second)
			select {
			case <-sleep:
				m.Run()
			case <-m.stop:
				return
			}
		}
	}()

	return nil
}

func (m *Monitor) Stop() error {
	select {
	case m.stop <- true:
	default:
	}

	return nil
}

func (m *Monitor) Add(dsn string) (c chan bool, err error) {
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

func (m *Monitor) Remove(dsn string, c chan bool) {
	m.Lock()
	defer m.Unlock()

	if mysqlInstance, ok := m.mysqlInstances[dsn]; ok {
		mysqlInstance.Remove(c)
		if mysqlInstance.Empty() {
			delete(m.mysqlInstances, dsn)
		}
	}
}

func (m *Monitor) Run() {
	m.RLock()
	defer m.RUnlock()

	for _, mysqlInstance := range m.mysqlInstances {
		if mysqlInstance.CheckIfMysqlRestarted() {
			mysqlInstance.NotifySubscribers()
		}
	}
}

func (m *Monitor) createMysqlInstance(dsn string) (mi *mysqlInstance, err error) {
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
