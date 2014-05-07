package factory

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	mysqlConn "github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/sysconfig"
	"github.com/percona/percona-agent/sysconfig/mysql"
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

func (f *Factory) Make(service string, instanceId uint, data []byte) (sysconfig.Monitor, error) {
	var monitor sysconfig.Monitor
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
		alias := "sysconfig-mysql-" + mysqlIt.Hostname

		// Make a MySQL sysconfig monitor.
		monitor = mysql.NewMonitor(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
			mysqlConn.NewConnection(mysqlIt.DSN),
		)
	default:
		return nil, errors.New("Unknown sysconfig monitor type: " + service)
	}
	return monitor, nil
}
