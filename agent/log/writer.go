package log

// Converts log message (strings) to log entries and sends to log relayer

import (
	"fmt"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type LogWriter struct {
	logChan chan *LogEntry
	service string
	user string	// proto.Msg.User
	id uint		// proto.Msg.Id
}

func NewLogWriter(logChan chan *LogEntry, service string) *LogWriter {
	l := &LogWriter{
		logChan: logChan,
		service: service,
	}
	return l
}

func (l *LogWriter) Re(msg *proto.Msg) {
	l.user = msg.User
	l. id = msg.Id
}

func (l *LogWriter) Debug(entry ...interface{}) {
	l.log(LOG_LEVEL_DEBUG, entry);
}

func (l *LogWriter) Info(entry ...interface{}) {
	l.log(LOG_LEVEL_INFO, entry);
}

func (l *LogWriter) Warn(entry ...interface{}) {
	l.log(LOG_LEVEL_WARN, entry);
}

func (l *LogWriter) Error(entry ...interface{}) {
	l.log(LOG_LEVEL_ERROR, entry);
}

func (l *LogWriter) Fatal(entry ...interface{}) {
	l.log(LOG_LEVEL_FATAL, entry);
}

func (l *LogWriter) log(level uint, entry []interface{}) {
	fullMsg := ""
	for i, str := range entry {
		if i > 0 {
			fullMsg += " "
		}
		fullMsg += fmt.Sprintf("%v", str)
	}
	logEntry := &LogEntry{
		User: l.user,
		Id: l.id,
		Level: level,
		Service: l.service,
		Msg: fullMsg,
	}
	l.logChan <- logEntry
}
