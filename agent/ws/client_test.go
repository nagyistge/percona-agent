package ws_test

import (
	"time"
	. "launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-cloud-tools/agent/ws"
	"github.com/percona/percona-cloud-tools/agent/proto"
	ws_server "github.com/percona/percona-cloud-tools/test/mock/ws-server"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	conn *ws.WsClient
	dataChan chan string // msgs from client
}
var _ = Suite(&TestSuite{})

const (
	ADDR = "127.0.0.1:8000" // make sure this port is free
	URL = "ws://" + ADDR
	ENDPOINT = "/"
)

// Start a mock ws server that sends all client msgs back to us via dataChan.
func (s *TestSuite) SetUpSuite(t *C) {
	s.dataChan = make(chan string, 10)
	mockWsServer := new(ws_server.MockWsServer)
	go mockWsServer.Run(ADDR, ENDPOINT, s.dataChan)
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
}

func (s *TestSuite) TearDownTest(t *C) {
	if s.conn != nil {
		s.conn.Disconnect()
	}
}

/*
 * The server, client, and tests are concurrent, so waitForData() is required
 * because the client can send a msg and a test can check dataChan for that msg
 * before the server has sent it to the chan.
 */
func waitForData(s *TestSuite) []string {
	var buf []string
	var haveData bool = true
	for haveData {
		select {
		case data := <-s.dataChan:
			buf = append(buf, data)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

/*
 * Test sending messages
 */

func (s *TestSuite) TestSend(t *C) {
	// A simple, built-in message without data
	s.conn.Send(proto.Ping())
	expect := []string{
		`{"cmd":"ping"}`,
	}
	got := waitForData(s)
	t.Check(got, DeepEquals, expect)

	// A more complex, realistic message with data
	data := make(map[string]string)
	data["api-key"] = "123abc"
	data["username"] = "root"
	msg := proto.NewMsg("connect", data)
	s.conn.Send(msg)
	expect = []string{
		`{"cmd":"connect","data":"{\"api-key\":\"123abc\",\"username\":\"root\"}"}`,
	}
	got = waitForData(s)
	t.Check(got, DeepEquals, expect)
}
