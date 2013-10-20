package log

// Relays log entries from log writers to the client, filters on log level

import (
	"os"
	golog "log"
	"sync"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type LogRelayer struct {
	client proto.Client
	logChan chan *LogEntry
	logger *golog.Logger
	level uint
	loggerMutex *sync.Mutex
}

func OpenLogFile(logFile string) (*golog.Logger, error) {
	file, err := os.OpenFile(logFile, os.O_WRONLY | os.O_APPEND | os.O_CREATE, 0755)
	if err != nil {
		return nil, err
	}
	newLogger := golog.New(file, "", golog.LstdFlags)
	return newLogger, nil
}

func NewLogRelayer(client proto.Client, logChan chan *LogEntry, logger *golog.Logger, level uint) *LogRelayer {
	r := &LogRelayer{
		client: client,
		logChan: logChan,
		logger: logger,
		level: level,
		loggerMutex: new(sync.Mutex),
	}
	return r
}

func (r *LogRelayer) SetLogLevel(levelName string) error {
	var level uint
	switch levelName {
	case "debug":
		level = LOG_LEVEL_DEBUG
	case "info":
		level = LOG_LEVEL_INFO
	case "warn":
		level = LOG_LEVEL_WARN
	case "error":
		level = LOG_LEVEL_ERROR
	case "fatal":
		level = LOG_LEVEL_FATAL
	default:
		return InvalidLogLevelError{Level:levelName}
	}
	r.level = level // @todo this may be unsafe since Run() is reading it
	return nil
}

func (r *LogRelayer) SetLogFile(logFile string) error {
	newLogger, err := OpenLogFile(logFile)
	if err != nil {
		return err
	}
	r.loggerMutex.Lock()
	r.logger = newLogger
	r.loggerMutex.Unlock()
	return nil
}

func (r *LogRelayer) Run() {
	for entry := range r.logChan {
		if entry.Level >= r.level {
			if err := r.client.Send(entry); err != nil {
				r.logger.Printf("ERROR: %s\n", err)
				r.logger.Println(entry)
			}
		}
		if entry.Level == LOG_LEVEL_FATAL {
			r.logger.Fatal("FATAL: %s\n", entry)
		}
	}
}

