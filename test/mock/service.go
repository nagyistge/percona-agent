package mock

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
)

type MockServiceManager struct {
	name         string
	traceChan    chan string
	readyChan    chan bool
	StartErr     error
	StopErr      error
	IsRunningVal bool
	status       *pct.Status
}

func NewMockServiceManager(name string, readyChan chan bool, traceChan chan string) *MockServiceManager {
	m := &MockServiceManager{
		name:      name,
		readyChan: readyChan,
		traceChan: traceChan,
		status:    pct.NewStatus([]string{name}),
	}
	return m
}

func (m *MockServiceManager) Start() error {
	m.traceChan <- fmt.Sprintf("Start %s", m.name)
	// Return when caller is ready.  This allows us to simulate slow starts.
	m.status.Update(m.name, "Starting")
	<-m.readyChan
	m.IsRunningVal = true
	m.status.Update(m.name, "Ready")
	return m.StartErr
}

func (m *MockServiceManager) Stop() error {
	m.traceChan <- "Stop " + m.name
	// Return when caller is ready.  This allows us to simulate slow stops.
	m.status.Update(m.name, "Stopping")
	<-m.readyChan
	m.IsRunningVal = false
	m.status.Update(m.name, "Stopped")
	return m.StopErr
}

func (m *MockServiceManager) Status() map[string]string {
	m.traceChan <- "Status " + m.name
	return m.status.All()
}

func (m *MockServiceManager) GetConfig() ([]proto.AgentConfig, error) {
	return nil, nil
}

func (m *MockServiceManager) IsRunning() bool {
	m.traceChan <- "IsRunning " + m.name
	return m.IsRunningVal
}

func (m *MockServiceManager) Handle(cmd *proto.Cmd) *proto.Reply {
	return nil
}

func (m *MockServiceManager) Reset() {
	m.status.Update(m.name, "")
}
