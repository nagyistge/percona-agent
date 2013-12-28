package mock

import (
	"code.google.com/p/go.net/websocket"
	proto "github.com/percona/cloud-protocol"
)

type NullClient struct {
	conn *websocket.Conn
}

func NewNullClient() *NullClient {
	c := &NullClient{
		conn: new(websocket.Conn),
	}
	return c
}

func (c *NullClient) Connect() error {
	return nil
}

func (c *NullClient) Disconnect() error {
	return nil
}

func (c *NullClient) Run() {
}

func (c *NullClient) SendChan() chan *proto.Reply {
	return nil
}

func (c *NullClient) RecvChan() chan *proto.Cmd {
	return nil
}

func (c *NullClient) Send(data interface{}) error {
	return nil
}

func (c *NullClient) Recv(data interface{}) error {
	return nil
}

func (c *NullClient) Do(cmd *proto.Cmd) error {
	return nil
}

func (c *NullClient) ErrorChan() chan error {
	return nil
}

func (c *NullClient) Conn() *websocket.Conn {
	return c.conn
}
