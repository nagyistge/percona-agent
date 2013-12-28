package client

import (
	"code.google.com/p/go.net/websocket"
	proto "github.com/percona/cloud-protocol"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

type WebsocketClient struct {
	origin   string
	url      string
	config   *websocket.Config
	conn     *websocket.Conn
	recvChan chan *proto.Cmd
	sendChan chan *proto.Reply
	errChan  chan error
}

func NewWebsocketClient(url string, origin string) (*WebsocketClient, error) {
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		return nil, err
	}
	c := &WebsocketClient{
		origin:   origin,
		url:      url,
		config:   config,
		conn:     nil,
		recvChan: make(chan *proto.Cmd, RECV_BUFFER_SIZE),
		sendChan: make(chan *proto.Reply, SEND_BUFFER_SIZE),
		errChan:  make(chan error, 2),
	}
	return c, nil
}

func (c *WebsocketClient) Connect() error {
	conn, err := websocket.DialConfig(c.config)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *WebsocketClient) Disconnect() error {
	var err error
	if c.conn != nil {
		close(c.sendChan)
		err = c.conn.Close()
		c.conn = nil
	}
	return err
}

// @goroutine
func (c *WebsocketClient) Run() {
	/**
	 * If either Send or Recieve causes an error, send the error
	 * to errChan.  The caller should watch this channel and take
	 * action, e.g. reconnect client.
	 */

	// Receive Reply from client and send to API.
	go func() {
		var err error
		defer func() { c.errChan <- err }()
		for reply := range c.sendChan {
			if err = c.Send(reply); err != nil {
				break
			}
		}
	}()

	// Receive Cmd from API and send to client.
	var err error
	defer func() { c.errChan <- err }()
	for {
		cmd := new(proto.Cmd)
		if err = c.Recv(cmd); err != nil {
			break
		}
		c.recvChan <- cmd
	}
}

func (c *WebsocketClient) SendChan() chan *proto.Reply {
	return c.sendChan
}

func (c *WebsocketClient) RecvChan() chan *proto.Cmd {
	return c.recvChan
}

func (c *WebsocketClient) Send(data interface{}) error {
	/**
	 * I cannot provoke an EOF error on websocket.Send(), only Receive().
	 * Perhaps EOF errors are only reported on recv?  This only affects
	 * the logger since it's ws send-only: it will need a goroutine blocking
	 * on Recieve() that, upon error, notifies the sending goroutine
	 * to reconnect.
	 */
	return websocket.JSON.Send(c.conn, data)
}

func (c *WebsocketClient) Recv(data interface{}) error {
	return websocket.JSON.Receive(c.conn, data)
}

func (c *WebsocketClient) ErrorChan() chan error {
	return c.errChan
}

func (c *WebsocketClient) Conn() *websocket.Conn {
	return c.conn
}
