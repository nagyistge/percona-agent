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

package monitor

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mm"
	"github.com/percona/percona-agent/mm/mysql"
	"github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/mrms"
	mysqlConn "github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
)

type Factory struct {
	logChan chan *proto.LogEntry
	ir      *instance.Repo
	mrm     mrms.Monitor
}

func NewFactory(logChan chan *proto.LogEntry, ir *instance.Repo, mrm mrms.Monitor) *Factory {
	f := &Factory{
		logChan: logChan,
		ir:      ir,
		mrm:     mrm,
	}
	return f
}

func (f *Factory) Make(service string, instanceId uint, data []byte) (mm.Monitor, error) {
	var monitor mm.Monitor
	switch service {
	case "mysql":
		// Load the MySQL instance info (DSN, name, etc.).
		mysqlIt := &proto.MySQLInstance{}
		if err := f.ir.Get(service, instanceId, mysqlIt); err != nil {
			return nil, err
		}

		// Parse the MySQL sysconfig config.
		config := &mysql.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// The user-friendly name of the service, e.g. sysconfig-mysql-db101:
		alias := "mm-mysql-" + mysqlIt.Hostname

		// Make a MySQL metrics monitor.
		monitor = mysql.NewMonitor(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
			mysqlConn.NewConnection(mysqlIt.DSN),
			f.mrm,
		)
	case "server":
		// Parse the system mm config.
		config := &system.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// Only one system for now, so no SystemInstance and no  "-instanceName" suffix.
		alias := "mm-system"

		// Make a MySQL metrics monitor.
		monitor = system.NewMonitor(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
		)
	default:
		return nil, errors.New("Unknown metrics monitor type: " + service)
	}
	return monitor, nil
}
