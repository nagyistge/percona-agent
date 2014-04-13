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

package client_test

import (
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	. "launchpad.net/gocheck"
	"testing"
	"time"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
	server  *mock.WebsocketServer
	api     *mock.API
}

var _ = Suite(&TestSuite{})

const (
	ADDR     = "127.0.0.1:8000" // make sure this port is free
	URL      = "ws://" + ADDR
	ENDPOINT = "/"
)

func (s *TestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "ws")

	mock.SendChan = make(chan interface{}, 5)
	mock.RecvChan = make(chan interface{}, 5)
	s.server = new(mock.WebsocketServer)
	go s.server.Run(ADDR, ENDPOINT)
	time.Sleep(100 * time.Millisecond)

	links := map[string]string{"agent": URL}
	s.api = mock.NewAPI("http://localhost", ADDR, "apikey", "uuid", links)
}

func (s *TestSuite) TearDownTest(t *C) {
	// Disconnect all clients.
	for _, c := range mock.Clients {
		mock.DisconnectClient(c)
	}
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestSend(t *C) {
	/**
	 * LogRelay (logrelay/) uses "direct" interface, not send/recv chans.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	// Client sends state of connection (true=connected, false=disconnected)
	// on its ConnectChan.
	connected := false
	doneChan := make(chan bool)
	go func() {
		connected = <-ws.ConnectChan()
		doneChan <- true
	}()

	// Wait for connection in mock ws server.
	ws.Connect()
	c := <-mock.ClientConnectChan

	<-doneChan
	t.Check(connected, Equals, true)

	// Send a log entry.
	logEntry := &proto.LogEntry{
		Level:   2,
		Service: "qan",
		Msg:     "Hello",
	}
	err = ws.Send(logEntry, 5)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChan)
	t.Assert(len(got), Equals, 1)

	// We're dealing with generic data.
	m := got[0].(map[string]interface{})
	t.Check(m["Level"], Equals, float64(2))
	t.Check(m["Service"], Equals, "qan")
	t.Check(m["Msg"], Equals, "Hello")

	// Quick check that Conn() works.
	conn := ws.Conn()
	t.Check(conn, NotNil)

	// Status should report connected to the proper link.
	status := ws.Status()
	t.Check(status, DeepEquals, map[string]string{
		"ws":      "Connected " + URL,
		"ws-link": URL,
	})

	// Disconnect should not return an error.
	err = ws.Disconnect()
	t.Assert(err, IsNil)

	// Status should report disconnected and still the proper link.
	status = ws.Status()
	t.Check(status, DeepEquals, map[string]string{
		"ws":      "Disconnected",
		"ws-link": URL,
	})
}

func (s *TestSuite) TestChannels(t *C) {
	/**
	 * Agent uses send/recv channels instead of "direct" interface.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	// Start send/recv chans, but idle until successful Connect.
	ws.Start()
	defer ws.Stop()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// API sends Cmd to client.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd

	// If client's recvChan is working, it will receive the Cmd.
	got := test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	t.Assert(got[0], DeepEquals, *cmd)

	// Client sends Reply in response to Cmd.
	reply := cmd.Reply(nil, nil)
	ws.SendChan() <- reply

	// If client's sendChan is working, we/API will receive the Reply.
	data := test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)

	// We're dealing with generic data again.
	m := data[0].(map[string]interface{})
	t.Assert(m["Cmd"], Equals, "Status")
	t.Assert(m["Error"], Equals, "")

	err = ws.Disconnect()
	t.Assert(err, IsNil)
}

func (s *TestSuite) TestApiDisconnect(t *C) {
	/**
	 * If using direct interface, Recv() should return error if API disconnects.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// No error yet.
	got := test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	mock.DisconnectClient(c)

	/**
	 * I cannot provoke an error on websocket.Send(), only Receive().
	 * Perhaps errors (e.g. ws closed) are only reported on recv?
	 * This only affect the logger since it's ws send-only: it will
	 * need a goroutine blocking on Recieve() that, upon error, notifies
	 * the sending goroutine to reconnect.
	 */
	var data interface{}
	err = ws.Recv(data, 5)
	t.Assert(err, NotNil) // EOF due to disconnect.
}

func (s *TestSuite) TestChannelsApiDisconnect(t *C) {
	/**
	 * If using chnanel interface, ErrorChan() should return error if API disconnects.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	var gotErr error
	doneChan := make(chan bool)
	go func() {
		gotErr = <-ws.ErrorChan()
		doneChan <- true
	}()

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	// No error yet.
	select {
	case <-doneChan:
		t.Error("No error yet")
	default:
	}

	mock.DisconnectClient(c)

	// Wait for error.
	select {
	case <-doneChan:
		t.Check(gotErr, NotNil) // EOF due to disconnect.
	case <-time.After(1 * time.Second):
		t.Error("Get error")
	}
}

func (s *TestSuite) TestErrorChan(t *C) {
	/**
	 * When client disconnects due to send or recv error,
	 * it should send the error on its ErrorChan().
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// No error yet.
	got := test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	// API sends Cmd to client.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd

	// No error yet.
	got = test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	// Disconnect the client.
	mock.DisconnectClient(c)

	// Client should send error from disconnect.
	got = test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 1)
	t.Assert(got[0], NotNil)

	err = ws.Disconnect()
	t.Assert(err, IsNil)
}

func (s *TestSuite) TestConnectBackoff(t *C) {
	/**
	 * Connect() should wait between attempts, using pct.Backoff (pct/backoff.go).
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()
	defer ws.Disconnect()

	// 0s wait, connect, err="Lost connection",
	// 1s wait, connect, err="Lost connection",
	// 3s wait, connect, ok
	t0 := time.Now()
	for i := 0; i < 2; i++ {
		mock.DisconnectClient(c)
		ws.Connect()
		c = <-mock.ClientConnectChan
		<-ws.ConnectChan() // connect ack
	}
	d := time.Now().Sub(t0)
	if d < time.Duration(3*time.Second) {
		t.Errorf("Exponential backoff wait time between connect attempts: %s\n", d)
	}
}

func (s *TestSuite) TestChannelsAfterReconnect(t *C) {
	/**
	 * Client send/recv chans should work after disconnect and reconnect.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent")
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	// Send cmd and wait for reply to ensure we're fully connected.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd
	got := test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	reply := cmd.Reply(nil, nil)
	ws.SendChan() <- reply
	data := test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)

	// Disconnect client.
	mock.DisconnectClient(c)
	<-ws.ConnectChan() // disconnect ack

	// Reconnect client and send/recv again.
	ws.Connect()
	c = <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	c.SendChan <- cmd
	got = test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	reply = cmd.Reply(nil, nil)
	ws.SendChan() <- reply
	data = test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)
}
