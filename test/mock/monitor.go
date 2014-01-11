package mock

import (
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/mm"
)

type Monitor struct {
	ReadyChan chan bool
	running bool
}

func NewMonitor() *Monitor {
	m := &Monitor{
	}
	return m
}

func (m *Monitor) Start(config []byte, ticker pct.Ticker, collectionChan chan *mm.Collection) error {
	if m.ReadyChan != nil {
		<-m.ReadyChan
	}
	m.running = true
	return nil
}

func (m *Monitor) Stop() error {
	m.running = false
	return nil
}

func (m *Monitor) Status() string {
	if m.running {
		return "Running"
	}
	return "Stopped"
}
