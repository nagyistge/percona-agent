package proto_test

import (
	. "launchpad.net/gocheck"
	"testing"
	. "github.com/percona/percona-cloud-tools/agent/proto"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{}
var _ = Suite(&TestSuite{})

func (s *TestSuite) TestNewMsg(t *C) {
	data := make(map[string]string)
	data["msg"] = "It crashed!"
	got := NewMsg("err", data)
	expect := &Msg{
		Cmd: "err",
		Data: `{"msg":"It crashed!"}`,
	}
	t.Check(got, DeepEquals, expect)
}

