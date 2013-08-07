package ws_client

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type MockClient struct {
	fromClient chan *proto.Msg
	toClient chan *proto.Msg
}

func NewMockClient(fromClient chan *proto.Msg, toClient chan *proto.Msg) *MockClient {
	c := &MockClient{
		fromClient: fromClient,
		toClient: toClient,
	}
	return c
}

func (c *MockClient) Connect() error {
	return nil
}

func (c *MockClient) Disconnect() error {
	return nil
}

func (c *MockClient) Send(msg *proto.Msg) error {
	c.fromClient <- msg
	return nil
}

func (c *MockClient) Recv() (*proto.Msg, error) {
	select {
	case msg := <-c.toClient:
		return msg, nil
	default:
		return nil, nil
	}
}
