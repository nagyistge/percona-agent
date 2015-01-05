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

package service

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
)

const (
	SERVICE_NAME = "explain"
)

type Explain struct {
	logger      *pct.Logger
	connFactory mysql.ConnectionFactory
	ir          *instance.Repo
}

func NewExplain(logger *pct.Logger, connFactory mysql.ConnectionFactory, ir *instance.Repo) *Explain {
	e := &Explain{
		logger:      logger,
		connFactory: connFactory,
		ir:          ir,
	}
	return e
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (e *Explain) Handle(cmd *proto.Cmd) *proto.Reply {
	// Get explain query
	explainQuery, err := e.getExplainQuery(cmd)
	if err != nil {
		return cmd.Reply(nil, err)
	}

	// The real name of the internal service, e.g. query-mysql-1:
	name := e.getInstanceName(explainQuery.Service, explainQuery.InstanceId)

	e.logger.Info("Running explain", name, cmd)

	// Create connector to MySQL instance
	conn, err := e.createConn(explainQuery.Service, explainQuery.InstanceId)
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

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (e *Explain) getInstanceName(service string, instanceId uint) (name string) {
	// The real name of the internal service, e.g. query-mysql-1:
	instanceName := e.ir.Name(service, instanceId)
	name = fmt.Sprintf("%s-%s", SERVICE_NAME, instanceName)
	return name
}

func (e *Explain) createConn(service string, instanceId uint) (conn mysql.Connector, err error) {
	// Load the MySQL instance info (DSN, name, etc.).
	mysqlIt := &proto.MySQLInstance{}
	if err = e.ir.Get(service, instanceId, mysqlIt); err != nil {
		return nil, err
	}

	// Create MySQL connection
	conn = e.connFactory.Make(mysqlIt.DSN)

	return conn, nil
}

func (e *Explain) getExplainQuery(cmd *proto.Cmd) (explainQuery *proto.ExplainQuery, err error) {
	if cmd.Data == nil {
		return nil, fmt.Errorf("%s.getExplainQuery:cmd.Data is empty", SERVICE_NAME)
	}

	if err := json.Unmarshal(cmd.Data, &explainQuery); err != nil {
		return nil, fmt.Errorf("%s.getExplainQuery:json.Unmarshal:%s", SERVICE_NAME, err)
	}

	return explainQuery, nil
}
