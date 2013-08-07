package log

// Converts log message (strings) to log entries and sends to log relayer

type LogWriter struct {
	logChan chan *LogEntry
	service string
}

func NewLogWriter(logChan chan *LogEntry, service string) *LogWriter {
	l := &LogWriter{
		logChan: logChan,
		service: service,
	}
	return l
}

func (l *LogWriter) Debug(entry string) {
	l.log(LOG_LEVEL_DEBUG, entry);
}

func (l *LogWriter) Info(entry string) {
	l.log(LOG_LEVEL_INFO, entry);
}

func (l *LogWriter) Warn(entry string) {
	l.log(LOG_LEVEL_WARN, entry);
}

func (l *LogWriter) Error(entry string) {
	l.log(LOG_LEVEL_ERROR, entry);
}

func (l *LogWriter) Fatal(entry string) {
	l.log(LOG_LEVEL_FATAL, entry);
}

func (l *LogWriter) log(level uint, entry string) {
	logEntry := &LogEntry{
		Level: level,
		Service: l.service,
		Entry: entry,
	}
	l.logChan <- logEntry
}
