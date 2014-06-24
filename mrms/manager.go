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
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
)

/**
 * PCT Manager for MRMS (MySQL Restart Monitoring Service)
 */

type Manager struct {
	logger  *pct.Logger
	monitor Monitor
	// --
	status *pct.Status
}

func NewManager(logger *pct.Logger, monitor Monitor) *Manager {
	m := &Manager{
		logger:  logger,
		monitor: monitor,
		// --
		status: pct.NewStatus([]string{"mrms"}),
	}
	return m
}

func (m *Manager) Start() error {
	err := m.monitor.Start()

	return err
}

func (m *Manager) Stop() error {
	err := m.monitor.Stop()
	return err
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}
