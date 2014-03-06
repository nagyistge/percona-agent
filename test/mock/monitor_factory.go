package mock

import (
	"github.com/percona/cloud-tools/mm"
)

type MonitorFactory struct {
	monitors  []mm.Monitor
	monitorNo int
	Made      []string
}

func NewMonitorFactory(monitors []mm.Monitor) *MonitorFactory {
	f := &MonitorFactory{
		monitors: monitors,
		Made:     []string{},
	}
	return f
}

func (f *MonitorFactory) Make(mtype, name string) (mm.Monitor, error) {
	f.Made = append(f.Made, mtype+"/"+name)
	if f.monitorNo > len(f.monitors) {
		return f.monitors[f.monitorNo-1], nil
	}
	nextMonitor := f.monitors[f.monitorNo]
	f.monitorNo++
	return nextMonitor, nil
}

func (f *MonitorFactory) Set(monitors []mm.Monitor) {
	f.monitorNo = 0
	f.monitors = monitors
	f.Made = []string{}
}
