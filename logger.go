package pct

import (
	"fmt"
	proto "github.com/percona/cloud-protocol"
	"os"
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
	l.log(proto.LOG_LEVEL_DEBUG, entry)
}

func (l *Logger) Info(entry ...interface{}) {
	l.log(proto.LOG_LEVEL_INFO, entry)
}

func (l *Logger) Warn(entry ...interface{}) {
	l.log(proto.LOG_LEVEL_WARN, entry)
}

func (l *Logger) Error(entry ...interface{}) {
	l.log(proto.LOG_LEVEL_ERROR, entry)
}

func (l *Logger) Fatal(entry ...interface{}) {
	l.log(proto.LOG_LEVEL_FATAL, entry)
	os.Exit(-1)
}

func (l *Logger) log(level uint, entry []interface{}) {
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
