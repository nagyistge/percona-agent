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

package instance

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
)

type Manager struct {
	logger    *pct.Logger
	configDir string
	// --
	status *pct.Status
	repo   *Repo
}

func NewManager(logger *pct.Logger, configDir string) *Manager {
	repo := NewRepo(pct.NewLogger(logger.LogChan(), "instance-repo"), configDir)
	m := &Manager{
		logger:    logger,
		configDir: configDir,
		// --
		status: pct.NewStatus([]string{"instance-manager"}),
		repo:   repo,
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	return m.repo.Init()
	m.status.Update("instance-manager", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	// Can't stop instance manager.
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("instance-manager", "Handling", cmd)
	defer m.status.Update("instance-manager", "Ready")

	it := &proto.ServiceInstance{}
	if err := json.Unmarshal(cmd.Data, it); err != nil {
		return cmd.Reply(nil, err)
	}

	switch cmd.Cmd {
	case "Add":
		err := m.repo.Add(it.Service, it.Id, it.Instance, true) // true = write to disk
		return cmd.Reply(nil, err)
	case "Remove":
		err := m.repo.Remove(it.Service, it.Id)
		return cmd.Reply(nil, err)
	default:
		// todo: dynamic config
		return cmd.Reply(pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) LoadConfig(configDir string) ([]byte, error) {
	return nil, nil
}

func (m *Manager) WriteConfig(config interface{}, name string) error {
	return nil
}

func (m *Manager) RemoveConfig(name string) error {
	return nil
}
