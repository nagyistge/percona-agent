package logrelay_test

import (
	"fmt"
	proto "github.com/percona/cloud-protocol"
	pct "github.com/percona/cloud-tools"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	. "launchpad.net/gocheck"
	"os"
	"testing"
	// Testing
	"github.com/percona/cloud-tools/logrelay"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logFile  string
	sendChan chan interface{}
	recvChan chan interface{}
	client   *mock.WebsocketClient
	relay   *logrelay.LogRelay
	logger  *pct.Logger
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.logFile = fmt.Sprintf("/tmp/logrelay_test.go.%d", os.Getpid())

	s.sendChan = make(chan interface{}, 5)
	s.recvChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendChan, s.recvChan)

	s.relay = logrelay.NewLogRelay(s.client)
	s.logger = pct.NewLogger(s.relay.LogChan(), "test")
	go s.relay.Run()
}

func (s *TestSuite) TearDownSuite(t *C) {
	os.Remove(s.logFile)
}

func (s *TestSuite) SetUpTest(t *C) {
	s.relay.LogLevelChan() <-proto.LOG_INFO
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *TestSuite) TestLogLevel(t *C) {
	r := s.relay
	l := s.logger

	r.LogLevelChan() <- proto.LOG_DEBUG
	l.Debug("debug")
	l.Info("info")
	l.Warn("warning")
	l.Error("error")
	l.Fatal("fatal")
	got := test.WaitLog(s.recvChan)
	expect := []proto.LogEntry{
		{Level: proto.LOG_DEBUG, Service: "test", Msg: "debug"},
		{Level: proto.LOG_INFO, Service: "test", Msg: "info"},
		{Level: proto.LOG_WARNING, Service: "test", Msg: "warning"},
		{Level: proto.LOG_ERROR, Service: "test", Msg: "error"},
		{Level: proto.LOG_CRITICAL, Service: "test", Msg: "fatal"},
	}
	t.Check(got, DeepEquals, expect)

	r.LogLevelChan() <- proto.LOG_WARNING
	l.Debug("debug")
	l.Info("info")
	l.Warn("warning")
	l.Error("error")
	l.Fatal("fatal")
	got = test.WaitLog(s.recvChan)
	expect = []proto.LogEntry{
		{Level: proto.LOG_WARNING, Service: "test", Msg: "warning"},
		{Level: proto.LOG_ERROR, Service: "test", Msg: "error"},
		{Level: proto.LOG_CRITICAL, Service: "test", Msg: "fatal"},
	}
	t.Check(got, DeepEquals, expect)
}
