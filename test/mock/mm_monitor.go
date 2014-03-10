package mock

import (
	"github.com/percona/cloud-tools/mm"
	"time"
)

type MmMonitorFactory struct {
	monitors  []mm.Monitor
	monitorNo int
	Made      []string
}

func NewMmMonitorFactory(monitors []mm.Monitor) *MmMonitorFactory {
	f := &MmMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *MmMonitorFactory) Make(mtype, name string) (mm.Monitor, error) {
	f.Made = append(f.Made, mtype+"/"+name)
	if f.monitorNo > len(f.monitors) {
		return f.monitors[f.monitorNo-1], nil
	}
	nextMonitor := f.monitors[f.monitorNo]
	f.monitorNo++
	return nextMonitor, nil
}

func (f *MmMonitorFactory) Set(monitors []mm.Monitor) {
	f.monitorNo = 0
	f.monitors = monitors
	f.Made = []string{}
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

func (m *MmMonitor) Start(config []byte, tickChan chan time.Time, collectionChan chan *mm.Collection) error {
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
