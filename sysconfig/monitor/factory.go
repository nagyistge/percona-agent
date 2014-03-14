package factory

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/instance"
	mysqlConn "github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sysconfig"
	"github.com/percona/cloud-tools/sysconfig/mysql"
)

type SysconfigFactory struct {
	logChan chan *proto.LogEntry
	im      *instance.Manager
}

func NewSysconfigFactory(logChan chan *proto.LogEntry, im *instance.Manager) *SysconfigFactory {
	f := &SysconfigFactory{
		logChan: logChan,
		im:      im,
	}
	return f
}

func (f *SysconfigFactory) Make(service string, instanceId uint, data []byte) (sysconfig.Monitor, error) {
	var monitor sysconfig.Monitor
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
