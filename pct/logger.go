package pct

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
)

type Logger struct {
	logChan chan *proto.LogEntry
	service string
	cmd     *proto.Cmd
}

func NewLogger(logChan chan *proto.LogEntry, service string) *Logger {
	l := &Logger{
		logChan: logChan,
		service: service,
	}
	return l
}

func (l *Logger) LogChan() chan *proto.LogEntry {
	return l.logChan
}

func (l *Logger) InResponseTo(cmd *proto.Cmd) {
	l.cmd = cmd
}

func (l *Logger) Debug(entry ...interface{}) {
	l.log(proto.LOG_DEBUG, entry)
}

func (l *Logger) Info(entry ...interface{}) {
	l.log(proto.LOG_INFO, entry)
}

func (l *Logger) Warn(entry ...interface{}) {
	l.log(proto.LOG_WARNING, entry)
}

func (l *Logger) Error(entry ...interface{}) {
	l.log(proto.LOG_ERROR, entry)
}

func (l *Logger) Fatal(entry ...interface{}) {
	l.log(proto.LOG_CRITICAL, entry)
}

func (l *Logger) log(level byte, entry []interface{}) {
	fullMsg := ""
	for i, str := range entry {
		if i > 0 {
			fullMsg += " "
		}
		fullMsg += fmt.Sprintf("%v", str)
	}
	logEntry := &proto.LogEntry{
		Level:   level,
		Service: l.service,
		Msg:     fullMsg,
	}
	if l.cmd != nil {
		logEntry.Cmd = fmt.Sprintf("%s", l.cmd)
	}
	l.logChan <- logEntry
}
