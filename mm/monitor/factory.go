package monitor

import (
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/mm/system"
	"github.com/percona/cloud-tools/pct"
)

type Factory struct {
	logChan chan *proto.LogEntry
}

func NewFactory(logChan chan *proto.LogEntry, im *instance.Manager) *Factory {
	f := &Factory{
		logChan: logChan,
		im:      im,
	}
	return f
}

func Make(service string, instanceId uint, data []byte) (Monitor, error) {
	var monitor mm.Monitor
	switch service {
	case "mysql":
		// Load the MySQL instance info (DSN, name, etc.).
		mysqlIt := &proto.MySQLInstance{}
		if err := f.im.Get(service, instanceId, mysqlIt); err != nil {
			return nil, err
		}

		// Parse the MySQL sysconfig config.
		config := &mysql.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// The user-friendly name of the service, e.g. sysconfig-mysql-db101:
		alias := "mm-mysql-" + mysqlIt.Name

		// Make a MySQL metrics monitor.
		monitor = mysql.NewMonitor(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
			mysqlConn.NewConnection(mysqlIt.DSN),
		)
	case "system":
		// Parse the system mm config.
		config := &system.Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}

		// Only one system for now, so no SystemInstance and no  "-instanceName" suffix.
		alias := "mm-system"

		// Make a MySQL metrics monitor.
		monitor = mysql.NewMonitor(
			alias,
			config,
			pct.NewLogger(f.logChan, alias),
		)
		monitor = system.NewMonitor(pct.NewLogger(f.logChan, name))
	default:
		return nil, errors.New("Unknown metrics monitor type: " + service)
	}
	return monitor, nil
}
