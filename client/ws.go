package client

import (
	"code.google.com/p/go.net/websocket"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
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
	started  bool
	sendSync *pct.SyncChan
	recvSync *pct.SyncChan
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
		sendSync: pct.NewSyncChan(),
		recvSync: pct.NewSyncChan(),
	}
	return c, nil
}

func (c *WebsocketClient) Start() {
	// Start send() and recv() goroutines, but they wait for successful Connect().
	go c.send()
	go c.recv()
	c.started = true
}

func (c *WebsocketClient) Stop() {
	if c.started {
		c.sendSync.Stop()
		c.recvSync.Stop()

		c.sendSync.Wait()
		c.recvSync.Wait()
	}
	c.started = false
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

	// Start/resume send() and recv() goroutines if Start() was called.
	if c.started {
		c.recvSync.Start()
		c.sendSync.Start()
	}

	// Success!
	c.backoff.Success()
	return nil
}

func (c *WebsocketClient) Disconnect() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
		c.conn = nil
	}
	return err
}

func (c *WebsocketClient) send() {
	// Receive Reply from client and send to API.
	defer c.sendSync.Done()
	for {
		// Wait to start (connect) or be told to stop.
		select {
		case <-c.sendSync.StartChan:
			c.sendSync.StartChan <- true
		case <-c.sendSync.StopChan:
			return
		}

	SEND_LOOP:
		for {
			select {
			case reply := <-c.sendChan:
				if err := c.Send(reply); err != nil {
					select {
					case c.errChan <- err:
					default:
					}
					break SEND_LOOP
				}
			case <-c.sendSync.StopChan:
				return
			}
		}
	}
}

func (c *WebsocketClient) recv() {
	// Receive Cmd from API and send to client.
	defer c.recvSync.Done()
	for {
		// Wait to start (connect) or be told to stop.
		select {
		case <-c.recvSync.StartChan:
			c.recvSync.StartChan <- true
		case <-c.recvSync.StopChan:
			return
		}

	RECV_LOOP:
		for {
			// Before blocking on Recv, see if we're supposed to stop.
			select {
			case <-c.recvSync.StopChan:
				return
			default:
			}

			cmd := &proto.Cmd{}
			if err := c.Recv(cmd); err != nil {
				select {
				case c.errChan <- err:
				default:
				}
				break RECV_LOOP
			}
			c.recvChan <- cmd
		}
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
