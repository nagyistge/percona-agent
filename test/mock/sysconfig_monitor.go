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
	"time"

	"github.com/percona/percona-agent/sysconfig"
)

type SysconfigMonitorFactory struct {
	monitors  []sysconfig.Monitor
	monitorNo int
	Made      []string
}

func NewSysconfigMonitorFactory(monitors []sysconfig.Monitor) *SysconfigMonitorFactory {
	f := &SysconfigMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *SysconfigMonitorFactory) Make(uuid string, data []byte) (sysconfig.Monitor, error) {
	f.Made = append(f.Made, uuid)
	if f.monitorNo > len(f.monitors) {
		return f.monitors[f.monitorNo-1], nil
	}
	nextMonitor := f.monitors[f.monitorNo]
	f.monitorNo++
	return nextMonitor, nil
}

func (f *SysconfigMonitorFactory) Set(monitors []sysconfig.Monitor) {
	f.monitorNo = 0
	f.monitors = monitors
	f.Made = []string{}
}

// --------------------------------------------------------------------------

type SysconfigMonitor struct {
	tickChan  chan time.Time
	ReadyChan chan bool
	running   bool
	config    interface{}
}

func NewSysconfigMonitor() *SysconfigMonitor {
	m := &SysconfigMonitor{}
	return m
}

func (m *SysconfigMonitor) Start(tickChan chan time.Time, reportChan chan *sysconfig.Report) error {
	if m.ReadyChan != nil {
		<-m.ReadyChan
	}
	m.running = true
	return nil
}

func (m *SysconfigMonitor) Stop() error {
	m.running = false
	return nil
}

func (m *SysconfigMonitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}

func (m *SysconfigMonitor) TickChan() chan time.Time {
	return m.tickChan
}

func (m *SysconfigMonitor) Config() interface{} {
	return m.config
}

func (m *SysconfigMonitor) SetConfig(v interface{}) {
	m.config = v
}
