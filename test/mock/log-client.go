package mock

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type MockLogClient struct {
	logEntriesChan chan interface{}
}

func NewMockLogClient(logEntriesChan chan interface{}) *MockLogClient {
	c := &MockLogClient{
		logEntriesChan: logEntriesChan,
	}
	return c
}

func (c *MockLogClient) Connect() error {
	return nil
}

func (c *MockLogClient) Run() {
}

func (c *MockLogClient) Disconnect() error {
	return nil
}

func (c *MockLogClient) Send(data interface{}) error {
	c.logEntriesChan <-data
	return nil
}

func (c *MockLogClient) Recv(data interface{}) error {
	return nil
}

func (c *MockLogClient) SendChan() chan *proto.Msg {
	return nil
}

func (c *MockLogClient) RecvChan() chan *proto.Msg {
	return nil
}
