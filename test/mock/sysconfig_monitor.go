package mock

import (
	"fmt"
	"github.com/percona/cloud-tools/sysconfig"
	"time"
)

type SysconfigMonitorFactory struct {
	monitors  []sysconfig.Monitor
	monitorNo int
	Made      []string
}

func NewSysconfigMonitorFactory(monitors []sysconfig.Monitor) *SysconfigMonitorFactory {
	f := &SysconfigMonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *SysconfigMonitorFactory) Make(service string, id uint, data []byte) (sysconfig.Monitor, error) {
	f.Made = append(f.Made, fmt.Sprintf("%s-%d", service, id))
	if f.monitorNo > len(f.monitors) {
		return f.monitors[f.monitorNo-1], nil
	}
	nextMonitor := f.monitors[f.monitorNo]
	f.monitorNo++
	return nextMonitor, nil
}

func (f *SysconfigMonitorFactory) Set(monitors []sysconfig.Monitor) {
	f.monitorNo = 0
	f.monitors = monitors
	f.Made = []string{}
}

// --------------------------------------------------------------------------

type SysconfigMonitor struct {
	tickChan  chan time.Time
	ReadyChan chan bool
	running   bool
	config    interface{}
}

func NewSysconfigMonitor() *SysconfigMonitor {
	m := &SysconfigMonitor{}
	return m
}

func (m *SysconfigMonitor) Start(tickChan chan time.Time, reportChan chan *sysconfig.Report) error {
	if m.ReadyChan != nil {
		<-m.ReadyChan
	}
	m.running = true
	return nil
}

func (m *SysconfigMonitor) Stop() error {
	m.running = false
	return nil
}

func (m *SysconfigMonitor) Status() map[string]string {
	status := make(map[string]string)
	if m.running {
		status["monitor"] = "Running"
	} else {
		status["monitor"] = "Stopped"
	}
	return status
}

func (m *SysconfigMonitor) TickChan() chan time.Time {
	return m.tickChan
}

func (m *SysconfigMonitor) Config() interface{} {
	return m.config
}

func (m *SysconfigMonitor) SetConfig(v interface{}) {
	m.config = v
}
