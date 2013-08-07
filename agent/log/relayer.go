package log

// Relays log entries from log writers to the client, filters on log level

import (
	golog "log"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type LogRelayer struct {
	client proto.Client
	logChan chan *LogEntry
	logFile *golog.Logger
	level uint
	buffer []*LogEntry
}

func NewLogRelayer(client proto.Client, logChan chan *LogEntry, logFile *golog.Logger, level uint) *LogRelayer {
	r := &LogRelayer{
		client: client,
		logChan: logChan,
		logFile: logFile,
		level: level,
		buffer: make([]*LogEntry, 100),
	}
	return r
}

func (r *LogRelayer) Run() {
	for entry := range r.logChan {
		if entry.Level >= r.level {
			msg := proto.NewMsg("log", entry)
			r.client.Send(msg)
			// todo buffer on err
		}
	}
}
