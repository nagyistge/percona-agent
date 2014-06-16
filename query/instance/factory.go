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
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	mysqlConn "github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/query"
	"github.com/percona/percona-agent/query/mysql"
)

type Factory struct {
	logChan chan *proto.LogEntry
	ir      *instance.Repo
}

func NewFactory(logChan chan *proto.LogEntry, ir *instance.Repo) *Factory {
	f := &Factory{
		logChan: logChan,
		ir:      ir,
	}
	return f
}

func (f *Factory) Make(service string, instanceId uint, data []byte) (query.Instance, error) {
	var instance query.Instance
	switch service {
	case mysql.INSTANCE_TYPE:
		// Load the MySQL instance info (DSN, name, etc.).
		mysqlIt := &proto.MySQLInstance{}
		if err := f.ir.Get(service, instanceId, mysqlIt); err != nil {
			return nil, err
		}

		// Parse the MySQL query config.
		config := &mysql.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// The user-friendly name of the service, e.g. query-mysql-db101:
		alias := fmt.Sprintf("%s-%s-%s", query.SERVICE_NAME, mysql.INSTANCE_TYPE, mysqlIt.Hostname)

		// Make a MySQL instance.
		instance = mysql.NewInstance(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
			mysqlConn.NewConnection(mysqlIt.DSN),
		)
	default:
		return nil, fmt.Errorf("Unknown %s instance type: %s", query.SERVICE_NAME, service)
	}
	return instance, nil
}
