package ws_client

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type MockClient struct {
	dataToClient chan interface{}
	dataFromClient chan interface{}
	msgToClient chan *proto.Msg
	msgFromClient chan *proto.Msg
	sendChan chan *proto.Msg
	recvChan chan *proto.Msg
}

func NewMockClient(dataToClient chan interface{}, dataFromClient chan interface{}, msgToClient chan *proto.Msg, msgFromClient chan *proto.Msg) *MockClient {
	c := &MockClient{
		dataToClient: dataToClient,
		dataFromClient: dataFromClient,
		msgToClient: msgToClient,
		msgFromClient: msgFromClient,
		recvChan: make(chan *proto.Msg, 10),
		sendChan: make(chan *proto.Msg, 10),
	}
	return c
}

func (c *MockClient) Connect() error {
	return nil
}

func (c *MockClient) Run() {
	go func() {
		for msg := range c.sendChan {
			c.msgFromClient <-msg
		}
	}()

	go func() {
		for msg := range c.msgToClient {
			c.recvChan <-msg
		}
	}()
}

func (c *MockClient) Disconnect() error {
	return nil
}

func (c *MockClient) Send(data interface{}) error {
	c.dataFromClient <-data
	return nil
}

func (c *MockClient) Recv(data interface{}) error {
	select {
	case data = <-c.dataToClient:
	default:
	}
	return nil
}

func (c *MockClient) SendChan() chan *proto.Msg {
	return c.sendChan
}

func (c *MockClient) RecvChan() chan *proto.Msg {
	return c.recvChan
}
