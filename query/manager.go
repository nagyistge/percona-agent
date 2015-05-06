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

package query

import (
	"sync"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/pct"
)

const (
	SERVICE_NAME = "query"
)

type Manager struct {
	logger  *pct.Logger
	explain Service
	// --
	running bool
	sync.Mutex
	// --
	status *pct.Status
}

func NewManager(logger *pct.Logger, explain Service) *Manager {
	m := &Manager{
		logger:  logger,
		explain: explain,
		// --
		status: pct.NewStatus([]string{SERVICE_NAME}),
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

	m.status.UpdateRe(SERVICE_NAME, "Handling", cmd)
	defer m.status.Update(SERVICE_NAME, "Running")

	switch cmd.Cmd {
	case "Explain":
		m.status.UpdateRe(SERVICE_NAME, "Running explain", cmd)
		return m.explain.Handle(cmd)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}
