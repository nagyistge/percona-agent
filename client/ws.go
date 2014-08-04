/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package client

import (
	"code.google.com/p/go.net/websocket"
	"crypto/tls"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"net"
	"sync"
	"time"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

type WebsocketClient struct {
	logger *pct.Logger
	api    pct.APIConnector
	link   string
	// --
	conn        *websocket.Conn
	recvChan    chan *proto.Cmd
	sendChan    chan *proto.Reply
	connectChan chan bool
	errChan     chan error
	backoff     *pct.Backoff
	started     bool
	sendSync    *pct.SyncChan
	recvSync    *pct.SyncChan
	mux         *sync.Mutex
	name        string
	status      *pct.Status
	connected   bool
}

func NewWebsocketClient(logger *pct.Logger, api pct.APIConnector, link string) (*WebsocketClient, error) {
	name := logger.Service()
	c := &WebsocketClient{
		logger: logger,
		api:    api,
		link:   link,
		// --
		conn:        nil,
		recvChan:    make(chan *proto.Cmd, RECV_BUFFER_SIZE),
		sendChan:    make(chan *proto.Reply, SEND_BUFFER_SIZE),
		connectChan: make(chan bool, 1),
		errChan:     make(chan error, 2),
		backoff:     pct.NewBackoff(5 * time.Minute),
		sendSync:    pct.NewSyncChan(),
		recvSync:    pct.NewSyncChan(),
		mux:         new(sync.Mutex),
		name:        name,
		status:      pct.NewStatus([]string{name, name + "-link"}),
	}
	return c, nil
}

func (c *WebsocketClient) Start() {
	// Start send() and recv() goroutines, but they wait for successful Connect().
	if !c.started {
		go c.send()
		go c.recv()
		c.started = true
	}
}

func (c *WebsocketClient) Stop() {
	if c.started {
		c.sendSync.Stop()
		c.recvSync.Stop()
		c.sendSync.Wait()
		c.recvSync.Wait()
		c.started = false
	}
}

func (c *WebsocketClient) Connect() {
	c.logger.Debug("Connect:call")
	defer c.logger.Debug("Connect:return")

	for {
		// Wait before attempt to avoid DDoS'ing the API
		// (there are many other agents in the world).
		c.logger.Debug("Connect:backoff.Wait")
		c.status.Update(c.name, "Connect wait")
		time.Sleep(c.backoff.Wait())

		if err := c.ConnectOnce(10); err != nil {
			c.logger.Warn(err)
			continue
		}

		// Start/resume send() and recv() goroutines if Start() was called.
		if c.started {
			c.recvSync.Start()
			c.sendSync.Start()
		}

		c.backoff.Success()
		c.notifyConnect(true)
		return // success
	}
}

func (c *WebsocketClient) ConnectOnce(timeout uint) error {
	c.logger.Debug("ConnectOnce:call")
	defer c.logger.Debug("ConnectOnce:return")

	// Make websocket connection.  If this fails, either API is down or the ws
	// address is wrong.
	link := c.api.AgentLink(c.link)
	c.logger.Debug("ConnectOnce:link:" + link)
	config, err := websocket.NewConfig(link, c.api.Origin())
	if err != nil {
		return err
	}
	config.Header.Add("X-Percona-API-Key", c.api.ApiKey())

	c.status.Update(c.name, "Connecting "+link)
	conn, err := c.dialTimeout(config, timeout)
	if err != nil {
		return err
	}

	c.mux.Lock()
	defer c.mux.Unlock()
	c.connected = true
	c.conn = conn
	c.status.Update(c.name, "Connected "+link)

	return nil
}

func (c *WebsocketClient) dialTimeout(config *websocket.Config, timeout uint) (ws *websocket.Conn, err error) {
	c.logger.Debug("ConnectOnce:websocket.DialConfig:call")
	defer c.logger.Debug("ConnectOnce:websocket.DialConfig:return")

	// websocket.Dial() does not handle timeouts, so we use lower-level net package
	// to create connection with timeout, then create ws client with the net connection.

	if config.Location == nil {
		return nil, websocket.ErrBadWebSocketLocation
	}
	if config.Origin == nil {
		return nil, websocket.ErrBadWebSocketOrigin
	}

	var conn net.Conn
	switch config.Location.Scheme {
	case "ws":
		conn, err = net.DialTimeout("tcp", config.Location.Host, time.Duration(timeout)*time.Second)
	case "wss":
		dialer := &net.Dialer{
			Timeout: time.Duration(timeout) * time.Second,
		}
		if config.Location.Host == "localhost:8443" {
			// Test uses mock ws server which uses self-signed cert which causes Go to throw
			// an error like "x509: certificate signed by unknown authority".  This disables
			// the cert verification for testing.
			config.TlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", config.Location.Host, config.TlsConfig)
	default:
		err = websocket.ErrBadScheme
	}
	if err != nil {
		return nil, &websocket.DialError{config, err}
	}

	ws, err = websocket.NewClient(config, conn)
	if err != nil {
		return nil, err
	}

	return ws, nil
}

func (c *WebsocketClient) Disconnect() error {
	c.logger.DebugOffline("Disconnect:call")
	defer c.logger.DebugOffline("Disconnect:return")

	/**
	 * Must guard c.conn here to prevent duplicate notifyConnect() because Close()
	 * causes recv() to error which calls Disconnect(), and normally we want this:
	 * to call Disconnect() on recv error so that notifyConnect(false) is called
	 * to let user know that remote end hung up.  However, when user hangs up
	 * the Disconnect() call from recv() is duplicate and not needed.
	 */
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.connected {
		c.logger.DebugOffline("Disconnect:websocket.Conn.Close")
		if err := c.conn.Close(); err != nil {
			// Example: write tcp 127.0.0.1:8000: i/o timeout
			// That ^ can happen if remote end hangs up, then we call Close().
			// Since there's nothing we can do about errors here, we ignore them.
			c.logger.DebugOffline("Disconnect:websocket.Conn.Close:err:" + err.Error())
		}
		/**
		 * Do not set c.conn = nil to indicate that connection is closed because
		 * unless we also guard c.conn in Send() and Recv() c.conn.Set*Deadline()
		 * will panic.  If the underlying websocket.Conn is closed, then
		 * Set*Deadline() will do nothing and websocket.JSON.Send/Receive() will
		 * just return an error, which is a lot better than a panic.
		 */
		c.connected = false
		c.logger.DebugOffline("Disconnect:disconnected")
		c.status.Update(c.name, "Disconnected")
		c.notifyConnect(false)
	}

	return nil
}

func (c *WebsocketClient) send() {
	/**
	 * Send Reply from agent to API.
	 */

	c.logger.DebugOffline("send:call")
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("Recovered while sending reply to API: ", r)
		}
	}()
	defer c.logger.DebugOffline("send:return")
	defer c.sendSync.Done()

	for {
		// Wait to start (connect) or be told to stop.
		c.logger.DebugOffline("send:wait:start")
		select {
		case <-c.sendSync.StartChan:
			c.sendSync.StartChan <- true
		case <-c.sendSync.StopChan:
			return
		}

	SEND_LOOP:
		for {
			c.logger.DebugOffline("send:idle")
			select {
			case reply := <-c.sendChan:
				// Got Reply from agent, send to API.
				c.logger.DebugOffline("send:reply:", reply)
				if err := c.Send(reply, 10); err != nil {
					c.logger.DebugOffline("send:err:", err)
					select {
					case c.errChan <- err:
					default:
					}
					break SEND_LOOP
				}
			case <-c.sendSync.StopChan:
				c.logger.DebugOffline("send:stop")
				return
			}
		}

		c.logger.DebugOffline("send:Disconnect")
		c.Disconnect()
	}
}

func (c *WebsocketClient) recv() {
	/**
	 * Receive Cmd from API, forward to agent.
	 */

	c.logger.DebugOffline("recv:call")
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("Recovered while recieveing cmd from API: ", r)
		}
	}()
	defer c.logger.DebugOffline("recv:return")
	defer c.recvSync.Done()

	for {
		// Wait to start (connect) or be told to stop.
		c.logger.DebugOffline("recv:wait:start")
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
				c.logger.DebugOffline("recv:stop")
				return
			default:
			}

			// Wait for Cmd from API.
			cmd := &proto.Cmd{}
			if err := c.Recv(cmd, 0); err != nil {
				c.logger.DebugOffline("recv:err:", err)
				select {
				case c.errChan <- err:
				default:
				}
				break RECV_LOOP
			}

			// Forward Cmd to agent.
			c.logger.DebugOffline("recv:cmd:", cmd)
			c.recvChan <- cmd
		}

		c.logger.DebugOffline("recv:Disconnect")
		c.Disconnect()
	}
}

func (c *WebsocketClient) SendChan() chan *proto.Reply {
	return c.sendChan
}

func (c *WebsocketClient) RecvChan() chan *proto.Cmd {
	return c.recvChan
}

func (c *WebsocketClient) Send(data interface{}, timeout uint) error {
	// These make the debug output a little too verbose:
	// c.logger.DebugOffline("Send:call")
	// defer c.logger.DebugOffline("Send:return")
	if timeout > 0 {
		c.conn.SetWriteDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	} else {
		c.conn.SetWriteDeadline(time.Time{})
	}
	return websocket.JSON.Send(c.conn, data)
}

func (c *WebsocketClient) SendBytes(data []byte) error {
	c.logger.DebugOffline("SendBytes:call")
	defer c.logger.DebugOffline("SendBytes:return")
	c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return websocket.Message.Send(c.conn, data)
}

func (c *WebsocketClient) Recv(data interface{}, timeout uint) error {
	c.logger.DebugOffline("Recv:call")
	defer c.logger.DebugOffline("Recv:return")
	if timeout > 0 {
		t := time.Now().Add(time.Duration(timeout) * time.Second)
		c.conn.SetReadDeadline(t)
	} else {
		c.conn.SetReadDeadline(time.Time{})
	}
	return websocket.JSON.Receive(c.conn, data)
}

func (c *WebsocketClient) ConnectChan() chan bool {
	return c.connectChan
}

func (c *WebsocketClient) ErrorChan() chan error {
	return c.errChan
}

func (c *WebsocketClient) Conn() *websocket.Conn {
	return c.conn
}

func (c *WebsocketClient) Status() map[string]string {
	c.status.Update(c.name+"-link", c.api.AgentLink(c.link))
	return c.status.All()
}

func (c *WebsocketClient) notifyConnect(state bool) {
	c.logger.DebugOffline(fmt.Sprintf("notifyConnect:call:%t", state))
	defer c.logger.DebugOffline("notifyConnect:return")
	select {
	case c.connectChan <- state:
	case <-time.After(20 * time.Second):
		c.logger.Error("notifyConnect timeout")
	}
}
