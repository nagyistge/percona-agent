/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

package mock

import (
	"fmt"
	"github.com/percona/percona-agent/mm"
	"time"
)

type MmMonitorFactory struct {
	monitors  map[string]mm.Monitor
	monitorNo int
	Made      []string
}

func NewMmMonitorFactory(monitors map[string]mm.Monitor) *MmMonitorFactory {
	f := &MmMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *MmMonitorFactory) Make(service string, id uint, data []byte) (mm.Monitor, error) {
	name := fmt.Sprintf("%s-%d", service, id)
	if monitor, ok := f.monitors[name]; ok {
		return monitor, nil
	}

	panic(fmt.Sprintf("Mock factory doesn't have monitor %s. Provide it via NewMmMonitorFactory(...) or Set(...).", name))
}

// --------------------------------------------------------------------------

type MmMonitor struct {
	tickChan  chan time.Time
	ReadyChan chan bool
	running   bool
	config    interface{}
}

func NewMmMonitor() *MmMonitor {
	m := &MmMonitor{}
	return m
}

func (m *MmMonitor) Start(tickChan chan time.Time, collectionChan chan *mm.Collection) error {
	if m.ReadyChan != nil {
		<-m.ReadyChan
	}
	m.running = true
	return nil
}

func (m *MmMonitor) Stop() error {
	m.running = false
	return nil
}

func (m *MmMonitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}

func (m *MmMonitor) TickChan() chan time.Time {
	return m.tickChan
}

func (m *MmMonitor) Config() interface{} {
	return m.config
}

func (m *MmMonitor) SetConfig(v interface{}) {
	m.config = v
}
