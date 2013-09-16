package ws_test

import (
	"log"
	"time"
	. "launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-cloud-tools/agent/ws"
	"github.com/percona/percona-cloud-tools/agent/proto"
	. "github.com/percona/percona-cloud-tools/test"
	ws_server "github.com/percona/percona-cloud-tools/test/mock/ws-server"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	conn *ws.WsClient
	fromClients chan *proto.Msg // msgs from client
	toClients chan *proto.Msg // msgs from us to clients
	ping *proto.Msg
}
var _ = Suite(&TestSuite{})

const (
	ADDR = "127.0.0.1:8000" // make sure this port is free
	URL = "ws://" + ADDR
	ENDPOINT = "/"
)

// Start a mock ws server that sends all client msgs back to us via fromClients.
func (s *TestSuite) SetUpSuite(t *C) {
	s.fromClients = make(chan *proto.Msg, 10)
	s.toClients = make(chan *proto.Msg, 10)
	mockWsServer := new(ws_server.MockWsServer)
	go mockWsServer.Run(ADDR, ENDPOINT, s.fromClients, s.toClients)
}

func (s *TestSuite) SetUpTest(t *C) {
	/*
	 * Connect the ws client to the mock ws server.  Because everything
	 * is concurrent, this will probably fail the first time because we
	 * can reach here before the server has started (which was started in
	 * SetUpSuite()).  If the server fails to start, the assertion here
	 * will fail and the test suite will fail early.
	 */
	c, err := ws.NewClient(URL, ENDPOINT)
	t.Assert(err, IsNil)
	for i := 0; i < 10; i++ {
		err = c.Connect()
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Assert(err, IsNil)
	s.conn = c

	// A fake Msg and fake Cmd.
	s.ping = &proto.Msg{
		Ts: time.Now(),
		User: "daniel",
		Id: 1,
		Cmd: "ping",
		Data: []byte("ping"),
	}
}

func (s *TestSuite) TearDownTest(t *C) {
	if s.conn != nil {
		s.conn.Disconnect()
	}
}

func getServerMsgs(s *TestSuite) []proto.Msg {
	/*
	 * websocket.Codec.Receive() blocks, so gorun this and send back any
	 * msgs via a channel so we can...
	 */
	var msgs = make(chan *proto.Msg)
	go func() {
		msg := new(proto.Msg)
		err := s.conn.Recv(msg)
		if err != nil {
			log.Print(err)
		} else {
			msgs <- msg
		}
	}()

	// ...use select and time.After() to timeout.
	var buf []proto.Msg
	select {
	case msg := <-msgs:
		buf = append(buf, *msg)
	case <-time.After(100 * time.Millisecond):
		break
	}

	return buf
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

/*
 * Test sending messages
 */

func (s *TestSuite) TestSend(t *C) {
	// A simple, built-in message without data
	s.conn.Send(s.ping)
	expect := []proto.Msg{
		*s.ping,
	}
	got := WaitForClientMsgs(s.fromClients)
	t.Check(got, DeepEquals, expect)
}

/*
 * Test receiving messages
 */

func (s *TestSuite) TestRecv(t *C) {
	// A simple, built-in message without data
	s.toClients <-s.ping
	expect := []proto.Msg{
		*s.ping,
	}
	got := getServerMsgs(s)
	t.Check(got, DeepEquals, expect)
}

/*
 * Test the high-level, channel-based interface.
 */

func (s *TestSuite) TestChannels(t *C) {
	// Run the send and recv channel listeners.
	s.conn.Run()

	// Send a msg to the send channel.
	sendChan := s.conn.SendChan()
	sendChan <-s.ping

	// And the client should send that msg.
	expect := []proto.Msg{
		*s.ping,
	}
	got := WaitForClientMsgs(s.fromClients)
	t.Check(got, DeepEquals, expect)
}
