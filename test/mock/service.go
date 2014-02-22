package mock

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
)

type MockServiceManager struct {
	name         string
	traceChan    chan string
	readyChan    chan bool
	StartErr     error
	StopErr      error
	IsRunningVal bool
	status       string
}

func NewMockServiceManager(name string, readyChan chan bool, traceChan chan string) *MockServiceManager {
	m := &MockServiceManager{
		name:      name,
		readyChan: readyChan,
		traceChan: traceChan,
		status:    "",
	}
	return m
}

func (m *MockServiceManager) Start(msg *proto.Cmd, config []byte) error {
	m.traceChan <- fmt.Sprintf("Start %s %s", m.name, string(config))
	// Return when caller is ready.  This allows us to simulate slow starts.
	m.status = "Starting"
	<-m.readyChan
	m.IsRunningVal = true
	m.status = "Ready"
	return m.StartErr
}

func (m *MockServiceManager) Stop(msg *proto.Cmd) error {
	m.traceChan <- "Stop " + m.name
	// Return when caller is ready.  This allows us to simulate slow stops.
	m.status = "Stopping"
	<-m.readyChan
	m.IsRunningVal = false
	m.status = "Stopped"
	return m.StopErr
}

func (m *MockServiceManager) Status() string {
	m.traceChan <- "Status " + m.name
	return m.status
}

func (m *MockServiceManager) InternalStatus() map[string]string {
	m.traceChan <- "Status " + m.name
	return map[string]string{m.name: m.status}
}

func (m *MockServiceManager) IsRunning() bool {
	m.traceChan <- "IsRunning " + m.name
	return m.IsRunningVal
}

func (m *MockServiceManager) Handle(cmd *proto.Cmd) error {
	return nil
}

func (m *MockServiceManager) Reset() {
	m.status = ""
}

func (m *MockServiceManager) LoadConfig(configDir string) (interface{}, error) {
	return nil, nil
}

func (m *MockServiceManager) WriteConfig(config interface{}, name string) error {
	return nil
}

func (m *MockServiceManager) RemoveConfig(name string) error {
	return nil
}
