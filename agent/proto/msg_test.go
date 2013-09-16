package proto_test

import (
	"time"
	. "launchpad.net/gocheck"
	"testing"
	. "github.com/percona/percona-cloud-tools/agent/proto"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}
var _ = Suite(&TestSuite{})

func (s *TestSuite) TestReply(t *C) {
	now := time.Now()

	msg := &Msg{
		Ts: now,
		User: "daniel",
		Id: 1,
		Cmd: "StartService",
		Timeout: 10,
		Data: []byte("..."),
	}

	reply := &CmdReply{
		Error: nil,
	}

	got := msg.Reply(reply)
	expect := &Msg{
		User: "daniel",
		Id: 1,
		Cmd: "StartService",
		Data: []byte(`{"Error":null}`),
	}
	t.Check(got, DeepEquals, expect)
}

