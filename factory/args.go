package factory

import (
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sid"
	"github.com/percona/cloud-tools/ticker"
)

type ArgsFactory interface {
	GetClock() ticker.Manager
	GetSpooler() data.Spooler
	GetSID() sid.Manager
	MakeLogger(service string) *pct.Logger
	MakeMySQLConnector() mysql.Connector
}

type CommonArgsFactory struct {
	clock   ticker.Manager
	spool   data.Spooler
	sid     sid.Manager
	logChan chan *proto.LogEntry
}

func (f *CommonArgsFactory) GetClock() ticker.Manager {
	return f.clock
}

func (f *CommonArgsFactory) GetSpooler() data.Spooler {
	return f.spool
}

func (f *CommonArgsFactory) GetSID() sid.Manager {
	return f.sid
}

func (f *CommonArgsFactory) MakeLogger(service string) *pct.Logger {
	return pct.NewLogger(f.logChan, service)
}

func (f *CommonArgsFactory) MakeMySQLConnector() mysql.Connector {
	return &mysql.Connection{}
}
