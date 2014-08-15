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

package summary

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"sync"
)

const (
	SERVICE_NAME = "summary"
)

type Manager struct {
	logger *pct.Logger
	// --
	service    map[string]Service
	running    bool
	sync.Mutex // This manager is single threaded, this lock protects usage from multiple goroutines
	// --
	status *pct.Status
}

func NewManager(logger *pct.Logger) *Manager {
	m := &Manager{
		logger: logger,
		// --
		service: make(map[string]Service),
		status:  pct.NewStatus([]string{SERVICE_NAME}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.Lock()
	defer m.Unlock()

	if m.running {
		return pct.ServiceIsRunningError{Service: SERVICE_NAME}
	}

	m.running = true
	m.logger.Info("Started")
	m.status.Update(SERVICE_NAME, "Running")
	return nil
}

func (m *Manager) Stop() error {
	// Can't stop this manager.
	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.Lock()
	defer m.Unlock()

	if !m.running {
		return cmd.Reply(nil, pct.ServiceIsNotRunningError{Service: SERVICE_NAME})
	}

	m.status.UpdateRe(SERVICE_NAME, "Handling", cmd)
	defer m.status.Update(SERVICE_NAME, "Running")

	serviceName := cmd.Cmd
	if m.service[serviceName] == nil {
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}

	m.status.UpdateRe(SERVICE_NAME, fmt.Sprintf("Running %s", serviceName), cmd)
	return m.service[serviceName].Handle(cmd)
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) RegisterService(serviceName string, service Service) (err error) {
	m.Lock()
	defer m.Unlock()

	if m.service[serviceName] != nil {
		return fmt.Errorf("%s already registered", serviceName)
	}

	m.service[serviceName] = service
	return nil
}
