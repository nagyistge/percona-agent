package mock

import (
	"fmt"
	proto "github.com/percona/cloud-protocol"
)

type MockServiceManager struct {
	name         string
	traceChan    chan string
	readyChan    chan bool
	StartErr     error
	StopErr      error
	IsRunningVal bool
}

func NewMockServiceManager(name string, readyChan chan bool, traceChan chan string) *MockServiceManager {
	m := new(MockServiceManager)
	m.name = name
	m.readyChan = readyChan
	m.traceChan = traceChan
	return m
}

func (m *MockServiceManager) Start(msg *proto.Cmd, config []byte) error {
	m.traceChan <- fmt.Sprintf("Start %s %s", m.name, string(config))
	// Return when caller is ready.  This allows us to simulate slow starts.
	<-m.readyChan
	return m.StartErr
}

func (m *MockServiceManager) Stop(msg *proto.Cmd) error {
	m.traceChan <- "Stop " + m.name
	// Return when caller is ready.  This allows us to simulate slow stops.
	<-m.readyChan
	return m.StopErr
}

func (m *MockServiceManager) Status() string {
	m.traceChan <- "Status " + m.name
	return "AOK"
}

func (m *MockServiceManager) IsRunning() bool {
	m.traceChan <- "IsRunning " + m.name
	return m.IsRunningVal
}

func (m *MockServiceManager) Do(cmd *proto.Cmd) error {
	return nil
}
