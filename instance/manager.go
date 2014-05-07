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
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"strings"
)

type Manager struct {
	logger    *pct.Logger
	configDir string
	// --
	status *pct.Status
	repo   *Repo
}

func NewManager(logger *pct.Logger, configDir string, api pct.APIConnector) *Manager {
	repo := NewRepo(pct.NewLogger(logger.LogChan(), "instance-repo"), configDir, api)
	m := &Manager{
		logger:    logger,
		configDir: configDir,
		// --
		status: pct.NewStatus([]string{"instance", "instance-repo"}),
		repo:   repo,
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start() error {
	m.status.Update("instance", "Starting")
	if err := m.repo.Init(); err != nil {
		return err
	}
	m.logger.Info("Started")
	m.status.Update("instance", "Running")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop() error {
	// Can't stop the instance manager.
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("instance", "Handling", cmd)
	defer m.status.Update("instance", "Running")

	it := &proto.ServiceInstance{}
	if err := json.Unmarshal(cmd.Data, it); err != nil {
		return cmd.Reply(nil, err)
	}

	switch cmd.Cmd {
	case "Add":
		err := m.repo.Add(it.Service, it.InstanceId, it.Instance, true) // true = write to disk
		return cmd.Reply(nil, err)
	case "Remove":
		err := m.repo.Remove(it.Service, it.InstanceId)
		return cmd.Reply(nil, err)
	case "GetInfo":
		info, err := m.handleGetInfo(it.Service, it.Instance)
		return cmd.Reply(info, err)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	m.status.Update("instance-repo", strings.Join(m.repo.List(), " "))
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) Repo() *Repo {
	return m.repo
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) handleGetInfo(service string, data []byte) (interface{}, error) {
	switch service {
	case "mysql":
		it := &proto.MySQLInstance{}
		if err := json.Unmarshal(data, it); err != nil {
			return nil, errors.New("instance.Repo:json.Unmarshal:" + err.Error())
		}
		if it.DSN == "" {
			return nil, fmt.Errorf("MySQL instance DSN is not set")
		}
		if err := GetMySQLInfo(it); err != nil {
			return nil, err
		}
		return it, nil
	default:
		return nil, fmt.Errorf("Don't know how to get info for %s service", service)
	}
}

func GetMySQLInfo(it *proto.MySQLInstance) error {
	conn := mysql.NewConnection(it.DSN)
	if err := conn.Connect(1); err != nil {
		return err
	}
	sql := "SELECT /* percona-agent */" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	err := conn.DB().QueryRow(sql).Scan(
		&it.Hostname,
		&it.Distro,
		&it.Version,
	)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}
