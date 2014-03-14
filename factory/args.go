package factory

import (
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/ticker"
)

type ArgsFactory interface {
	MakeLogger(service string) *pct.Logger
	GetClock() ticker.Manager
	GetSpooler() data.Spooler
	GetInstanceManager() *instance.Manager
}

type CommonArgsFactory struct {
	logChan chan *proto.LogEntry
	clock   ticker.Manager
	spool   data.Spooler
	im      *instance.Manager
}

func NewCommonArgsFactory(logChan chan *proto.LogEntry, clock ticker.Manager, spool data.Spooler, im *instance.Manager) *CommonArgsFactory {
	f := &CommonArgsFactory{
		logChan: logChan,
		clock:   clock,
		spool:   spool,
		im:      im,
	}
	return f
}

func (f *CommonArgsFactory) MakeLogger(service string) *pct.Logger {
	return pct.NewLogger(f.logChan, service)
}

func (f *CommonArgsFactory) GetClock() ticker.Manager {
	return f.clock
}

func (f *CommonArgsFactory) GetSpooler() data.Spooler {
	return f.spool
}

func (f *CommonArgsFactory) GetInstanceManager() *instance.Manager {
	return f.im
}
