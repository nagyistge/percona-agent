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

package mysql

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/pct/cmd"
)

const (
	SERVICE_NAME     = "mysql"
	PT_SLEEP_SECONDS = "4"
)

type MySQL struct {
	CmdName string
	logger  *pct.Logger
	ir      *instance.Repo
}

func NewMySQL(logger *pct.Logger, ir *instance.Repo) *MySQL {
	return &MySQL{
		CmdName: "pt-mysql-summary",
		logger:  logger,
		ir:      ir,
	}
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *MySQL) Handle(protoCmd *proto.Cmd) *proto.Reply {
	// Get service instance
	serviceInstance, err := getServiceInstance(protoCmd)
	if err != nil {
		return protoCmd.Reply(nil, err)
	}

	// Load the MySQL instance info (DSN, name, etc.).
	mysqlIt := &proto.MySQLInstance{}
	if err = m.ir.Get(serviceInstance.Service, serviceInstance.InstanceId, mysqlIt); err != nil {
		return protoCmd.Reply(nil, err)
	}

	// Parse DSN to get user, pass, host, port, socket as separate fields
	// @todo parsing DSN should be as a method on proto.MySQLInstance.DSN or at least parser should be injected
	dsn, err := NewDSN(mysqlIt.DSN)
	if err != nil {
		return protoCmd.Reply(nil, err)
	}

	// Create params for ptMySQLSummary
	args := CreateParamsForPtMySQLSummary(dsn)

	// Run ptMySQLSummary with params
	ptMySQLSummary := cmd.NewRealCmd(m.CmdName, args...)
	output, err := ptMySQLSummary.Run()
	if err != nil {
		m.logger.Error(fmt.Sprintf("%s: %s", m.CmdName, err))
	}

	result := &proto.SysinfoResult{
		Raw: output,
	}

	return protoCmd.Reply(result, err)
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func getServiceInstance(protoCmd *proto.Cmd) (serviceInstance *proto.ServiceInstance, err error) {
	if protoCmd.Data == nil {
		return nil, fmt.Errorf("%s.getMySQLInstance:cmd.Data is empty", SERVICE_NAME)
	}

	if err := json.Unmarshal(protoCmd.Data, &serviceInstance); err != nil {
		return nil, fmt.Errorf("%s.getMySQLInstance:json.Unmarshal:%s", SERVICE_NAME, err)
	}

	return serviceInstance, nil
}

func CreateParamsForPtMySQLSummary(dsn *DSN) (args []string) {
	args = append(args, "--sleep", PT_SLEEP_SECONDS)
	if dsn.user != "" {
		args = append(args, "--user", dsn.user)
	}
	if dsn.passwd != "" {
		args = append(args, "--password", dsn.passwd)
	}
	if dsn.net == "unix" {
		args = append(args, "--socket", dsn.addr)
	} else {
		if dsn.addr != "" {
			args = append(args, "--host", dsn.addr)
		}
		if dsn.port != "" {
			args = append(args, "--port", dsn.port)
		}
	}

	return args
}
