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

package mock

import (
	"fmt"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/query"
)

type QueryMonitorFactory struct {
	monitors  map[string]query.Monitor
	monitorNo int
	Made      []string
}

func NewQueryMonitorFactory(monitors map[string]query.Monitor) *QueryMonitorFactory {
	f := &QueryMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *QueryMonitorFactory) Make(service string, id uint, data []byte) (query.Monitor, error) {
	name := fmt.Sprintf("%s-%d", service, id)
	if monitor, ok := f.monitors[name]; ok {
		return monitor, nil
	}

	panic(fmt.Sprintf("Mock factory doesn't have monitor %s. Provide it via NewQueryMonitorFactory(...) or Set(...).", name))
}

// --------------------------------------------------------------------------

type QueryMonitor struct {
	running bool
	config  interface{}
	explain map[string]*mysql.Explain
}

func NewQueryMonitor() *QueryMonitor {
	m := &QueryMonitor{
		explain: make(map[string]*mysql.Explain),
	}
	return m
}

func (m *QueryMonitor) Start() error {
	m.running = true
	return nil
}

func (m *QueryMonitor) Stop() error {
	m.running = false
	return nil
}

func (m *QueryMonitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}

func (m *QueryMonitor) Config() interface{} {
	return m.config
}

func (m *QueryMonitor) SetConfig(v interface{}) {
	m.config = v
}

func (n *QueryMonitor) Explain(query string) (explain *mysql.Explain, err error) {
	explain, ok := n.explain[query]
	if !ok {
		return nil, fmt.Errorf("Explain output not set in mock")
	}

	return explain, nil
}

func (n *QueryMonitor) SetExplain(query string, explain *mysql.Explain) {
	n.explain[query] = explain
}
