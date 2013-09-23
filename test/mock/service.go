package mock

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type MockServiceManager struct {
	traceChan chan string
	StartErr error
	StopErr error
	IsRunningVal bool
}

func NewMockServiceManager(traceChan chan string) *MockServiceManager {
	m := new(MockServiceManager)
	m.traceChan = traceChan
	return m
}

func (m *MockServiceManager) Start(msg *proto.Msg, config []byte) error {
	m.traceChan <-"Start"
	return m.StartErr
}

func (m *MockServiceManager) Stop(msg *proto.Msg) error {
	m.traceChan <-"Stop"
	return m.StopErr
}

func (m *MockServiceManager) Status() string {
	m.traceChan <-"Status"
	return "AOK"
}

func (m *MockServiceManager) IsRunning() bool {
	m.traceChan <-"IsRunning"
	return m.IsRunningVal
}
