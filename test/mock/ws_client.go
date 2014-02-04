package mock

import (
	//"fmt"
	"code.google.com/p/go.net/websocket"
	"github.com/percona/cloud-protocol/proto"
)

type WebsocketClient struct {
	testSendChan     chan *proto.Cmd
	userRecvChan     chan *proto.Cmd
	userSendChan     chan *proto.Reply
	testRecvChan     chan *proto.Reply
	testSendDataChan chan interface{}
	testRecvDataChan chan interface{}
	conn             *websocket.Conn
	ErrChan          chan error
	RecvError        chan error
	ConnectChan      chan error
}

func NewWebsocketClient(sendChan chan *proto.Cmd, recvChan chan *proto.Reply, sendDataChan chan interface{}, recvDataChan chan interface{}) *WebsocketClient {
	c := &WebsocketClient{
		testSendChan:     sendChan,
		userRecvChan:     make(chan *proto.Cmd, 10),
		userSendChan:     make(chan *proto.Reply, 10),
		testRecvChan:     recvChan,
		testSendDataChan: sendDataChan,
		testRecvDataChan: recvDataChan,
		conn:             new(websocket.Conn),
		RecvError:        make(chan error),
	}
	return c
}

func (c *WebsocketClient) Connect() error {
	var err error
	if c.ConnectChan != nil {
		// Wait for test to send error to send to agent/user.
		err = <-c.ConnectChan
	}
	return err
}

func (c *WebsocketClient) Disconnect() error {
	return nil
}

func (c *WebsocketClient) Run() {
	go func() {
		for cmd := range c.testSendChan { // test sends cmd
			c.userRecvChan <- cmd // user receives cmd
		}
	}()

	go func() {
		for reply := range c.userSendChan { // user sends reply
			c.testRecvChan <- reply // test receives reply
		}
	}()
}

func (c *WebsocketClient) SendChan() chan *proto.Reply {
	return c.userSendChan
}

func (c *WebsocketClient) RecvChan() chan *proto.Cmd {
	return c.userRecvChan
}

func (c *WebsocketClient) Send(data interface{}) error {
	// Relay data from user to test.
	c.testRecvDataChan <- data
	return nil
}

func (c *WebsocketClient) Recv(data interface{}) error {
	// Relay data from test to user.
	select {
	case data = <-c.testSendDataChan:
	case err := <-c.RecvError:
		return err
	}
	return nil
}

func (c *WebsocketClient) ErrorChan() chan error {
	return c.ErrChan
}

func (c *WebsocketClient) Conn() *websocket.Conn {
	return c.conn
}
