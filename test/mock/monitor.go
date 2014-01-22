package mock

import (
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/pct"
)

type Monitor struct {
	ReadyChan chan bool
	running   bool
}

func NewMonitor() *Monitor {
	m := &Monitor{}
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

func (m *Monitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}
