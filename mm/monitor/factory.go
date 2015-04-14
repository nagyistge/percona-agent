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

func (f *Factory) Make(uuid string, data []byte) (mm.Monitor, error) {
	var monitor mm.Monitor
	it, err := f.ir.Get(uuid)
	if err != nil {
		return nil, err
	}
	switch it.Prefix {
	case "mysql":
		// Load the MySQL instance info (DSN, name, etc.).
		// Parse the MySQL sysconfig config.
		config := &mysql.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// The user-friendly name of the tool, e.g. sysconfig-mysql-db101:
		alias := "mm-mysql-" + it.Name

		// Make a MySQL metrics monitor.
		monitor = mysql.NewMonitor(
			uuid,
			config,
			pct.NewLogger(f.logChan, alias),
			mysqlConn.NewConnection(it.DSN),
			f.mrm,
		)
	case "os":
		// Parse the system mm config.
		config := &system.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// Only one os for now, so no "-instanceName" suffix.
		alias := "mm-os"

		// Make a MySQL metrics monitor.
		monitor = system.NewMonitor(
			uuid,
			config,
			pct.NewLogger(f.logChan, alias),
		)
	default:
		return nil, errors.New("Unknown metrics monitor for instance prefix: " + it.Prefix)
	}
	return monitor, nil
}
