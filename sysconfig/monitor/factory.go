package monitor

import (
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sysconfig"
	"github.com/percona/cloud-tools/sysconfig/mysql"
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

func (f *Factory) Make(mtype, name string) (sysconfig.Monitor, error) {
	var monitor sysconfig.Monitor
	switch mtype {
	case "mysql":
		monitor = mysql.NewMonitor(pct.NewLogger(f.logChan, name))
	default:
		return nil, errors.New("Unknown sysconfig monitor type: " + mtype)
	}
	return monitor, nil
}
