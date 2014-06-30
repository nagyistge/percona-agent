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
	mysqlInstances map[string]*MysqlInstance
	sync.RWMutex
	// --
	sync *pct.SyncChan
}

func NewMonitor(logger *pct.Logger, mysqlConnFactory mysql.ConnectionFactory) mrms.Monitor {
	m := &Monitor{
		logger:           logger,
		mysqlConnFactory: mysqlConnFactory,
		mysqlInstances:   make(map[string]*MysqlInstance),
		sync:             pct.NewSyncChan(),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

/**
 * Monitor for MySQL restart every *interval*
 */
func (m *Monitor) Start(interval time.Duration) error {
	go m.run(interval)
	return nil
}

func (m *Monitor) Stop() error {
	m.sync.Stop()
	m.sync.Wait()
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

	c = mysqlInstance.Subscribers.Add()

	return c, nil
}

func (m *Monitor) Remove(dsn string, c chan bool) {
	m.Lock()
	defer m.Unlock()

	if mysqlInstance, ok := m.mysqlInstances[dsn]; ok {
		mysqlInstance.Subscribers.Remove(c)
		if mysqlInstance.Subscribers.Empty() {
			delete(m.mysqlInstances, dsn)
		}
	}
}

func (m *Monitor) Check() {
	m.RLock()
	defer m.RUnlock()

	for _, mysqlInstance := range m.mysqlInstances {
		if mysqlInstance.CheckIfMysqlRestarted() {
			mysqlInstance.Subscribers.Notify()
		}
	}
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Monitor) run(interval time.Duration) {
	defer m.sync.Done()

	m.Check() // Immediately run first check
	for {
		select {
		case <-time.After(interval):
			m.Check()
		case <-m.sync.StopChan:
			return
		}
	}
}

func (m *Monitor) createMysqlInstance(dsn string) (mi *MysqlInstance, err error) {
	mysqlConn := m.mysqlConnFactory.Make(dsn)
	subscribers := NewSubscribers(m.logger)
	return NewMysqlInstance(m.logger, mysqlConn, subscribers)
}
