package mock

import (
	"fmt"
	"github.com/percona/cloud-tools/mm"
	"time"
)

type MmMonitorFactory struct {
	monitors  map[string]mm.Monitor
	monitorNo int
	Made      []string
}

func NewMmMonitorFactory(monitors map[string]mm.Monitor) *MmMonitorFactory {
	f := &MmMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *MmMonitorFactory) Make(service string, id uint, data []byte) (mm.Monitor, error) {
	name := fmt.Sprintf("%s-%d", service, id)
	if monitor, ok := f.monitors[name]; ok {
		return monitor, nil
	}

	panic(fmt.Sprintf("Mock factory doesn't have monitor %s. Provide it via NewMmMonitorFactory(...) or Set(...).", name))
}

// --------------------------------------------------------------------------

type MmMonitor struct {
	tickChan  chan time.Time
	ReadyChan chan bool
	running   bool
	config    interface{}
}

func NewMmMonitor() *MmMonitor {
	m := &MmMonitor{}
	return m
}

func (m *MmMonitor) Start(tickChan chan time.Time, collectionChan chan *mm.Collection) error {
	if m.ReadyChan != nil {
		<-m.ReadyChan
	}
	m.running = true
	return nil
}

func (m *MmMonitor) Stop() error {
	m.running = false
	return nil
}

func (m *MmMonitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}

func (m *MmMonitor) TickChan() chan time.Time {
	return m.tickChan
}

func (m *MmMonitor) Config() interface{} {
	return m.config
}

func (m *MmMonitor) SetConfig(v interface{}) {
	m.config = v
}
