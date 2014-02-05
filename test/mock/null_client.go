package mock

import (
	"code.google.com/p/go.net/websocket"
	"github.com/percona/cloud-protocol/proto"
)

type NullClient struct {
	conn        *websocket.Conn
	connectChan chan bool
	errChan     chan error
}

func NewNullClient() *NullClient {
	c := &NullClient{
		conn:        new(websocket.Conn),
		connectChan: make(chan bool),
		errChan:     make(chan error),
	}
	return c
}

func (c *NullClient) Connect() error {
	return nil
}

func (c *NullClient) Disconnect() error {
	return nil
}

func (c *NullClient) Start() {
}

func (c *NullClient) Stop() {
}

func (c *NullClient) SendChan() chan *proto.Reply {
	return nil
}

func (c *NullClient) RecvChan() chan *proto.Cmd {
	return nil
}

func (c *NullClient) SendBytes(data []byte) error {
	return nil
}

func (c *NullClient) Send(data interface{}) error {
	return nil
}

func (c *NullClient) Recv(data interface{}) error {
	return nil
}

func (c *NullClient) ErrorChan() chan error {
	return c.errChan
}

func (c *NullClient) ConnectChan() chan bool {
	return c.connectChan
}

func (c *NullClient) Conn() *websocket.Conn {
	return c.conn
}
