package monitor

import (
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/mm/system"
	"github.com/percona/cloud-tools/pct"
)

type Factory struct {
	logChan chan *proto.LogEntry
}

func NewFactory(logChan chan *proto.LogEntry) *Factory {
	f := &Factory{
		logChan: logChan,
	}
	return f
}

func Make(service string, instanceId uint, data []byte) (Monitor, error) {
	var monitor mm.Monitor
	switch service {
	case "mysql":
		monitor = mysql.NewMonitor(pct.NewLogger(f.logChan, name))
	case "system":
		monitor = system.NewMonitor(pct.NewLogger(f.logChan, name))
	default:
		return nil, errors.New("Unknown monitor type: " + service)
	}
	return monitor, nil
}
