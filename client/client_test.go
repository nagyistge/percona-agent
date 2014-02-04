package client_test

import (
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/client"
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
	origin string
	server *mock.WebsocketServer
	auth   *proto.AgentAuth
}

var _ = Suite(&TestSuite{})

const (
	ADDR     = "127.0.0.1:8000" // make sure this port is free
	URL      = "ws://" + ADDR
	ENDPOINT = "/"
)

func (s *TestSuite) SetUpSuite(t *C) {
	s.origin = "http://localhost"
	mock.SendChan = make(chan interface{}, 5)
	mock.RecvChan = make(chan interface{}, 5)
	s.server = new(mock.WebsocketServer)
	go s.server.Run(ADDR, ENDPOINT)
	time.Sleep(100 * time.Millisecond)

	s.auth = new(proto.AgentAuth) // todo
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

	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	// Connect should not return an error.
	err = ws.Connect()
	t.Assert(err, IsNil)

	// Wait for connection in mock ws server.
	c := <-mock.ClientConnectChan

	// Send a log entry.
	logEntry := &proto.LogEntry{
		Level:   2,
		Service: "qan",
		Msg:     "Hello",
	}
	err = ws.Send(logEntry)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChan)
	t.Assert(len(got), Equals, 1)

	// We're dealing with generic data.
	m := got[0].(map[string]interface{})
	t.Assert(m["Level"], Equals, float64(2))
	t.Assert(m["Service"], Equals, "qan")
	t.Assert(m["Msg"], Equals, "Hello")

	// Disconnect should not return an error.
	err = ws.Disconnect()
	t.Assert(err, IsNil)
}

func (s *TestSuite) TestChannels(t *C) {
	/**
	 * Agent uses send/recv channels instead of "direct" interface.
	 */

	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	// Start send/recv chans, but idle until successful Connect.
	ws.Start()
	defer ws.Stop()

	err = ws.Connect()
	t.Assert(err, IsNil)

	c := <-mock.ClientConnectChan

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
	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	err = ws.Connect()
	if err != nil {
		t.Fatal(err)
	}
	c := <-mock.ClientConnectChan

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
	err = ws.Recv(data)
	t.Assert(err, NotNil) // EOF due to disconnect.
}

func (s *TestSuite) TestErrorChan(t *C) {
	/**
	 * When client disconnects due to send or recv error,
	 * it should send the error on its ErrorChan().
	 */

	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()

	err = ws.Connect()
	t.Assert(err, IsNil)

	c := <-mock.ClientConnectChan

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

	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	err = ws.Connect()
	t.Assert(err, IsNil)
	defer ws.Disconnect()

	c := <-mock.ClientConnectChan

	// 0s wait, connect, err="Lost connection",
	// 1s wait, connect, err="Lost connection",
	// 3s wait, connect, ok
	t0 := time.Now()
	for i := 0; i < 2; i++ {
		mock.DisconnectClient(c)
		ws.Connect()
		c = <-mock.ClientConnectChan
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

	ws, err := client.NewWebsocketClient(URL+ENDPOINT, s.origin, s.auth)
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	err = ws.Connect()
	t.Assert(err, IsNil)

	c := <-mock.ClientConnectChan

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

	// Reconnect client and send/recv again.
	err = ws.Connect()
	t.Assert(err, IsNil)
	c = <-mock.ClientConnectChan

	c.SendChan <- cmd
	got = test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	reply = cmd.Reply(nil, nil)
	ws.SendChan() <- reply
	data = test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)
}
