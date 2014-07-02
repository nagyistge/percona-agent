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

package query

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"sync"
)

const (
	SERVICE_NAME = "query"
)

type Manager struct {
	logger      *pct.Logger
	connFactory mysql.ConnectionFactory
	ir          *instance.Repo
	// --
	running bool
	sync.Mutex
	// --
	status *pct.Status
}

func NewManager(logger *pct.Logger, connFactory mysql.ConnectionFactory, ir *instance.Repo) *Manager {
	m := &Manager{
		logger:      logger,
		connFactory: connFactory,
		ir:          ir,
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
		return m.explain(cmd)
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

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) explain(cmd *proto.Cmd) *proto.Reply {
	// Get explain query
	explainQuery, err := m.getExplainQuery(cmd)
	if err != nil {
		return cmd.Reply(nil, err)
	}

	// The real name of the internal service, e.g. query-mysql-1:
	name := m.getInstanceName(explainQuery.Service, explainQuery.InstanceId)

	m.status.UpdateRe(SERVICE_NAME, "Running explain", cmd)
	m.logger.Info("Running explain", name, cmd)

	// Create connector to MySQL instance
	conn, err := m.createConn(explainQuery.Service, explainQuery.InstanceId)
	if err != nil {
		return cmd.Reply(nil, fmt.Errorf("Unable to create connector for %s: %s", name, err))
	}
	defer conn.Close()

	// Connect to MySQL instance
	if err := conn.Connect(2); err != nil {
		return cmd.Reply(nil, fmt.Errorf("Unable to connect to %s: %s", name, err))
	}

	// Run explain
	explain, err := conn.Explain(explainQuery.Query, explainQuery.Db)
	if err != nil {
		return cmd.Reply(nil, fmt.Errorf("Explain failed for %s: %s", name, err))
	}

	return cmd.Reply(explain)
}

func (m *Manager) getInstanceName(service string, instanceId uint) (name string) {
	// The real name of the internal service, e.g. query-mysql-1:
	instanceName := m.ir.Name(service, instanceId)
	name = fmt.Sprintf("%s-%s", SERVICE_NAME, instanceName)
	return name
}

func (m *Manager) createConn(service string, instanceId uint) (conn mysql.Connector, err error) {
	// Load the MySQL instance info (DSN, name, etc.).
	mysqlIt := &proto.MySQLInstance{}
	if err = m.ir.Get(service, instanceId, mysqlIt); err != nil {
		return nil, err
	}

	// Create MySQL connection
	conn = m.connFactory.Make(mysqlIt.DSN)

	return conn, nil
}

func (m *Manager) getExplainQuery(cmd *proto.Cmd) (explainQuery *proto.ExplainQuery, err error) {
	if cmd.Data == nil {
		return nil, fmt.Errorf("%s.getExplainQuery:cmd.Data is empty", SERVICE_NAME)
	}

	if err := json.Unmarshal(cmd.Data, &explainQuery); err != nil {
		return nil, fmt.Errorf("%s.getExplainQuery:json.Unmarshal:%s", SERVICE_NAME, err)
	}

	return explainQuery, nil
}
