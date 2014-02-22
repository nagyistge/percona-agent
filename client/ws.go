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
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"time"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

type WebsocketClient struct {
	logger      *pct.Logger
	origin      string
	url         string
	auth        *proto.AgentAuth
	config      *websocket.Config
	conn        *websocket.Conn
	recvChan    chan *proto.Cmd
	sendChan    chan *proto.Reply
	connectChan chan bool
	errChan     chan error
	backoff     *pct.Backoff
	started     bool
	sendSync    *pct.SyncChan
	recvSync    *pct.SyncChan
}

func NewWebsocketClient(logger *pct.Logger, url string, origin string, auth *proto.AgentAuth) (*WebsocketClient, error) {
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		return nil, err
	}
	config.Header.Add("X-Percona-API-Key", auth.ApiKey)

	c := &WebsocketClient{
		logger: logger,
		url:    url,
		origin: origin,
		auth:   auth,
		// --
		config:      config,
		conn:        nil,
		recvChan:    make(chan *proto.Cmd, RECV_BUFFER_SIZE),
		sendChan:    make(chan *proto.Reply, SEND_BUFFER_SIZE),
		connectChan: make(chan bool, 1),
		errChan:     make(chan error, 2),
		backoff:     pct.NewBackoff(5 * time.Minute),
		sendSync:    pct.NewSyncChan(),
		recvSync:    pct.NewSyncChan(),
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
	for {
		// Wait before attempt to avoid DDoS'ing the API
		// (there are many other agents in the world).
		time.Sleep(c.backoff.Wait())
		if err := c.ConnectOnce(); err != nil {
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

func (c *WebsocketClient) ConnectOnce() error {
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
		conn.Close()
		return errors.New(fmt.Sprint("Send auth:", err))
	}

	// After we send our auth creds, API responds with AuthReponse: any error = auth failure.
	authResponse := new(proto.AuthResponse)
	if err := c.Recv(authResponse, 5); err != nil {
		// websocket error, not auth fail
		conn.Close()
		return errors.New(fmt.Sprint("Recv auth response:", err))
	}
	if authResponse.Error != "" {
		// auth fail (invalid API key, agent UUID, or combo of those)
		conn.Close()
		return errors.New(fmt.Sprint("Auth fail:", err))
	}

	return nil
}

func (c *WebsocketClient) Disconnect() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
		c.conn = nil
		c.notifyConnect(false)
	}
	return err
}

func (c *WebsocketClient) send() {
	/**
	 * Send Reply from agent to API.
	 */

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
				// Got Reply from agent, send to API.
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

		c.Disconnect()
	}
}

func (c *WebsocketClient) recv() {
	/**
	 * Receive Cmd from API, forward to agent.
	 */

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

			// Wait for Cmd from API.
			cmd := &proto.Cmd{}
			if err := c.Recv(cmd, 0); err != nil {
				select {
				case c.errChan <- err:
				default:
				}
				break RECV_LOOP
			}

			// Forward Cmd to agent.
			c.recvChan <- cmd
		}

		c.Disconnect()
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
	c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return websocket.JSON.Send(c.conn, data)
}

func (c *WebsocketClient) SendBytes(data []byte) error {
	c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	return websocket.Message.Send(c.conn, data)
}

func (c *WebsocketClient) Recv(data interface{}, timeout uint) error {
	if timeout > 0 {
		t := time.Now().Add(time.Duration(timeout) * time.Second)
		c.conn.SetReadDeadline(t)
	} else {
		c.conn.SetReadDeadline(time.Time{})
	}
	if err := websocket.JSON.Receive(c.conn, data); err != nil {
		return errors.New(fmt.Sprint("Recv:", err))
	}
	return nil
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

func (c *WebsocketClient) notifyConnect(state bool) {
	c.connectChan <- state
}

func (c *WebsocketClient) SetLogger(logger *pct.Logger) {
	c.logger = logger
}
