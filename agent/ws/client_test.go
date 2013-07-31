package ws_test

import (
	//"fmt"
	. "launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-cloud-tools/agent/ws"
	"github.com/percona/percona-cloud-tools/agent/proto"
	ws_server "github.com/percona/percona-cloud-tools/test/mock/ws-server"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}
var _ = Suite(&TestSuite{})

const (
	ADDR = "127.0.0.1:8000"
	URL = "ws://" + ADDR
	ENDPOINT = "/"
)


var doneChan = make(chan bool)
var dataChan = make(chan string)

func (s *TestSuite) SetUpSuite(t *C) {
	mockWsServer := new(ws_server.MockWsServer)
	go mockWsServer.Run(
		ADDR,
		ENDPOINT,
		dataChan,
		doneChan,
	)
}

/*
func (s *TestSuite) TestConnect(t *C) {
	c, err := ws.NewClient(URL, ENDPOINT)
	t.Assert(err, IsNil)

	c.Connect()
	t.Assert(err, IsNil)

	c.Disconnect()
	t.Assert(err, IsNil)
}
*/

func (s *TestSuite) TestSend(t *C) {
	c, err := ws.NewClient(URL, ENDPOINT)
	t.Assert(err, IsNil)

	c.Connect()
	t.Assert(err, IsNil)

	var data []string
	go func() {
		for msg := range dataChan {
			data = append(data, msg)
		}
	}()

	c.Send(proto.Ping())
	c.Disconnect()

	<-doneChan // wait for goroutine

	expect := []string{`{"Command":"ping","Data":""}`}
	t.Check(data, DeepEquals, expect)
}

