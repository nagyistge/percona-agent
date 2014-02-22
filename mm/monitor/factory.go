package monitor

import (
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
)

type Factory struct {
	logChan chan *proto.LogEntry
}

func (f *Factory) Make(mtype, name string) (Monitor, error) {
	var monitor Monitor
	switch mtype {
	case "mysql":
		monitor := mysql.NewMonitor(NewLogger(f.logChan, name))
	default:
		return nil, errors.New("Unknown monitor type: " + mtype)
	}
	return monitor, nil
}
