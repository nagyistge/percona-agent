package qh

import (
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent/service"
	"github.com/percona/percona-cloud-tools/agent/log"
)

const (
	NAME = "query-history"
)

type QhManager struct {
	log *log.LogWriter
	config *Config  // nil if not running
}

func NewQhManager(logChan chan *log.LogEntry) *QhManager {
	qh := &QhManager{
		log: log.NewLogWriter(logChan, ""),
		config: nil, // not running yet
	}
	return qh
}

func (qh *QhManager) Start(config []byte) error {
	if qh.config != nil {
		return service.ServiceIsRunningError{Service:NAME}
	}

	firstConfig := new(Config)
	if err := json.Unmarshal(config, firstConfig); err != nil {
		return err
	}

	qh.log.Info("start!")

	qh.config = firstConfig // success: service configured and running
	return nil
}

func (qh *QhManager) Stop() error {
	return nil
}

func (qh *QhManager) UpdateConfig(config []byte) error {
	return nil
}

func (qh *QhManager) Status() string {
	return "ok"
}

func (qh *QhManager) IsRunning() bool {
	if qh.config != nil {
		return true
	}
	return false
}
