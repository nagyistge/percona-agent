package client

import (
	"code.google.com/p/go.net/websocket"
	"errors"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-protocol/proto"
	"time"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

type WebsocketClient struct {
	origin   string
	url      string
	auth     *proto.AgentAuth
	config   *websocket.Config
	conn     *websocket.Conn
	recvChan chan *proto.Cmd
	sendChan chan *proto.Reply
	errChan  chan error
	backoff  *pct.Backoff
}

func NewWebsocketClient(url string, origin string, auth *proto.AgentAuth) (*WebsocketClient, error) {
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		return nil, err
	}
	c := &WebsocketClient{
		origin:   origin,
		url:      url,
		auth:     auth,
		config:   config,
		conn:     nil,
		recvChan: make(chan *proto.Cmd, RECV_BUFFER_SIZE),
		sendChan: make(chan *proto.Reply, SEND_BUFFER_SIZE),
		errChan:  make(chan error, 2),
		backoff:  pct.NewBackoff(5 * time.Minute),
	}
	return c, nil
}

func (c *WebsocketClient) Connect() error {
	// Potentially wait before attempt.  Caller probably has us in a for loop,
	// and if API is flapping, we don't want to connect too fast, too often.
	time.Sleep(c.backoff.Wait())

	// Make websocket connection.  If this fails, either API is down or the ws
	// address is wrong.
	conn, err := websocket.DialConfig(c.config)
	if err != nil {
		return err
	}
	c.conn = conn

	// First API expects from us is our authentication credentials.
	// If this fails, it's probably an internal API error, *not* failed auth
	// because that happens next...
	if err := c.Send(c.auth); err != nil {
		return err
	}

	// After we send our auth creds, API responds with AuthReponse: any error = auth failure.
	authResponse := new(proto.AuthResponse)
	if err := c.Recv(authResponse); err != nil {
		return err // websocket error, not auth fail
	}
	if authResponse.Error != "" {
		// auth fail (invalid API key, agent UUID, or combo of those)
		return errors.New(authResponse.Error)
	}

	// Success!
	c.backoff.Success()
	return nil
}

func (c *WebsocketClient) Disconnect() error {
	var err error
	if c.sendChan != nil {
		close(c.sendChan)
		c.sendChan = nil
	}
	if c.conn != nil {
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
		defer func() {
			select {
			case c.errChan <- err:
			default:
			}
		}()
		for reply := range c.sendChan {
			if err = c.Send(reply); err != nil {
				break
			}
		}
	}()

	// Receive Cmd from API and send to client.
	var err error
	defer func() {
		select {
		case c.errChan <- err:
		default:
		}
	}()
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
