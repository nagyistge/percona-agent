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

type QueryInstanceFactory struct {
	instances  map[string]query.Instance
	instanceNo int
	Made       []string
}

func NewQueryInstanceFactory(instances map[string]query.Instance) *QueryInstanceFactory {
	f := &QueryInstanceFactory{
		instances: instances,
		Made:      []string{},
	}
	return f
}

func (f *QueryInstanceFactory) Make(service string, id uint, data []byte) (query.Instance, error) {
	name := fmt.Sprintf("%s-%d", service, id)
	if instance, ok := f.instances[name]; ok {
		return instance, nil
	}

	panic(fmt.Sprintf("Mock factory doesn't have instance %s. Provide it via NewQueryInstanceFactory(...) or Set(...).", name))
}

// --------------------------------------------------------------------------

type QueryInstance struct {
	running bool
	config  interface{}
	explain map[string]*mysql.Explain
}

func NewQueryInstance() *QueryInstance {
	m := &QueryInstance{
		explain: make(map[string]*mysql.Explain),
	}
	return m
}

func (m *QueryInstance) Start() error {
	m.running = true
	return nil
}

func (m *QueryInstance) Stop() error {
	m.running = false
	return nil
}

func (m *QueryInstance) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["instance"] = "Running"
	} else {
		status["instance"] = "Stopped"
	}
	return status
}

func (m *QueryInstance) Config() interface{} {
	return m.config
}

func (m *QueryInstance) SetConfig(v interface{}) {
	m.config = v
}

func (n *QueryInstance) Explain(query string) (explain *mysql.Explain, err error) {
	explain, ok := n.explain[query]
	if !ok {
		return nil, fmt.Errorf("Explain output not set in mock")
	}

	return explain, nil
}

func (n *QueryInstance) SetExplain(query string, explain *mysql.Explain) {
	n.explain[query] = explain
}
