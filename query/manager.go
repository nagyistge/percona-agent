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
	"encoding/json"
	"fmt"
	"sync"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	mysqlExec "github.com/percona/percona-agent/query/mysql"
)

const (
	SERVICE_NAME = "query"
)

type Manager struct {
	logger       *pct.Logger
	instanceRepo *instance.Repo
	connFactory  mysql.ConnectionFactory
	// --
	running bool
	sync.Mutex
	status *pct.Status
}

func NewManager(logger *pct.Logger, instanceRepo *instance.Repo, connFactory mysql.ConnectionFactory) *Manager {
	m := &Manager{
		logger:       logger,
		instanceRepo: instanceRepo,
		connFactory:  connFactory,
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
	m.status.Update(SERVICE_NAME, "Idle")
	return nil
}

func (m *Manager) Stop() error {
	// Let user stop this tool in case they don't want agent executing queries.
	m.Lock()
	defer m.Unlock()
	if !m.running {
		return nil
	}
	m.running = false
	m.logger.Info("Stopped")
	m.status.Update(SERVICE_NAME, "Stopped")
	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.Lock()
	defer m.Unlock()

	// Don't query if this tool is stopped.
	if !m.running {
		return cmd.Reply(nil, pct.ServiceIsNotRunningError{})
	}

	m.status.UpdateRe(SERVICE_NAME, "Handling", cmd)
	defer m.status.Update(SERVICE_NAME, "Idle")

	// See which type of subsystem this query is for. Right now we only support
	// MySQL, but this abstraction will make adding other subsystems easy.
	si := &proto.ServiceInstance{}
	if err := json.Unmarshal(cmd.Data, si); err != nil {
		return cmd.Reply(nil, err)
	}

	switch si.Service {
	case "mysql":
		return m.handleMySQLQuery(cmd, si)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{si.Service})
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

// --------------------------------------------------------------------------

func (m *Manager) handleMySQLQuery(cmd *proto.Cmd, si *proto.ServiceInstance) *proto.Reply {
	// Connect to MySQL.
	mysqlIt := &proto.MySQLInstance{}
	if err := m.instanceRepo.Get(si.Service, si.InstanceId, mysqlIt); err != nil {
		return cmd.Reply(nil, err)
	}
	conn := m.connFactory.Make(mysqlIt.DSN)
	if err := conn.Connect(1); err != nil {
		return cmd.Reply(nil, fmt.Errorf("Cannot connect to MySQL: %s", err))
	}
	defer conn.Close()

	// Create a MySQL query executor to do the actual work.
	e := mysqlExec.NewQueryExecutor(conn)

	// Get the instance name, e.g. mysql-db01, to make status human-readable.
	instanceName := m.instanceRepo.Name(si.Service, si.InstanceId)

	// Execute the query.
	switch cmd.Cmd {
	case "Explain":
		m.status.Update(SERVICE_NAME, "EXPLAIN query on "+instanceName)
		q := &proto.ExplainQuery{}
		if err := json.Unmarshal(cmd.Data, q); err != nil {
			return cmd.Reply(nil, err)
		}
		res, err := e.Explain(q.Db, q.Query)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("EXPLAIN failed: %s", err))
		}
		return cmd.Reply(res, nil)
	case "TableInfo":
		m.status.Update(SERVICE_NAME, "Table Info queries on "+instanceName)
		tableInfo := &proto.TableInfoQuery{}
		if err := json.Unmarshal(cmd.Data, tableInfo); err != nil {
			return cmd.Reply(nil, err)
		}
		res, err := e.TableInfo(tableInfo)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("Table Info failed: %s", err))
		}
		return cmd.Reply(res, nil)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}
