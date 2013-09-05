package qh

import (
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent/service"
	"github.com/percona/percona-cloud-tools/agent/log"
)

const (
	Name = "qh-manager"
)

type QhManager struct {
	log *log.LogWriter
	config *Config  // nil if not running
}

func NewQhManager(logChan chan *log.LogEntry) *QhManager {
	m := &QhManager{
		log: log.NewLogWriter(logChan, "qh-manager"),
		config: nil, // not running yet
	}
	return m
}

func (m *QhManager) Start(config []byte) error {
	if m.config != nil {
		return service.ServiceIsRunningError{Service:Name}
	}

	c := new(Config)
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.log.Info("Starting")

	// Prepare MySQL based on the config

	// Run goroutine to run workers

	m.config = c
	return nil
}

func (m *QhManager) Stop() error {
	return nil
}

func (m *QhManager) UpdateConfig(config []byte) error {
	return nil
}

func (m *QhManager) Status() string {
	return "ok"
}

func (m *QhManager) IsRunning() bool {
	if m.config != nil {
		return true
	}
	return false
}
