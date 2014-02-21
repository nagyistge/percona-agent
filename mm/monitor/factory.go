package monitor

import (
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/pct"
)

type RealFactory struct {
	logChan chan *proto.LogEntry
}

func (f *RealFactory) Make(mtype, name string) (Monitor, error) {
	var monitor Monitor
	switch mtype {
	case "mysql":
		monitor := mysql.NewMonitor(NewLogger(f.logChan, name+"-monitor"))
	default:
		return nil, errors.New("Unknown monitor type: " + mtype)
	}
	return monitor, nil
}
