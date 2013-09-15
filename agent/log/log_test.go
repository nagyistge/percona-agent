package log_test

import (
	"log"
	"time"
	"os"
	"fmt"
	. "launchpad.net/gocheck"
	"testing"
	"github.com/percona/percona-cloud-tools/test/mock/ws-client"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/ws"
	. "github.com/percona/percona-cloud-tools/test"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	dataToClient chan interface{}
	dataFromClient chan interface{}
	msgToClient chan *proto.Msg
	msgFromClient chan *proto.Msg
	fileName string
	logFile *log.Logger
	mockWsClient *ws_client.MockClient
	wsClient *ws.WsClient
	logChan chan *agentLog.LogEntry
}
var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.fileName = fmt.Sprintf("/tmp/log_test.go.%d", os.Getpid())
	file, err := os.Create(s.fileName)
	if err != nil {
		log.Fatal(err)
	}
	s.logFile = log.New(file, "test", 0)

	s.dataToClient = make(chan interface{}, 100)
	s.dataFromClient = make(chan interface{}, 100)
	s.msgToClient = make(chan *proto.Msg, 100)
	s.msgFromClient = make(chan *proto.Msg, 100)
	s.mockWsClient = ws_client.NewMockClient(s.dataToClient, s.dataFromClient, s.msgToClient, s.msgFromClient)

	s.logChan = make(chan *agentLog.LogEntry, 100)
}

func (s *TestSuite) TearDownSuite(t *C) {
	os.Remove(s.fileName)
}

func getLogEntries(s *TestSuite) []agentLog.LogEntry {
	var buf []agentLog.LogEntry
	var haveData bool = true
	for haveData {
		select {
		case msg := <-s.logChan:
			buf = append(buf, *msg)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *TestSuite) TestLogEntries(t *C) {
	l := agentLog.NewLogWriter(s.logChan, "test")

	l.Debug("debug")
	got := getLogEntries(s)
	expect := []agentLog.LogEntry{
		{
			Level: 1,
			Service: "test",
			Msg: "debug",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Info("info")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 2,
			Service: "test",
			Msg: "info",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Warn("warning")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 3,
			Service: "test",
			Msg: "warning",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Error("error")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 4,
			Service: "test",
			Msg: "error",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Fatal("fatal")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 5,
			Service: "test",
			Msg: "fatal",
		},
	}
	t.Check(got, DeepEquals, expect)
}

func (s *TestSuite) TestReLogEntry(t *C) {
	l := agentLog.NewLogWriter(s.logChan, "test")

	now := time.Now()
	msg := &proto.Msg{
		Ts: now,
		User: "daniel",
		Id: 1,
		Cmd: "start-service",
		Data: nil,
		Timeout: 60,
	}

	// Associate log messages with this proto.Msg:
	l.Re(msg)

	l.Info("start")
	got := getLogEntries(s)
	expect := []agentLog.LogEntry{
		{
			User: "daniel",
			Id: 1,
			Level: 2,
			Service: "test",
			Msg: "start",
		},
	}
	t.Check(got, DeepEquals, expect)
}

/////////////////////////////////////////////////////////////////////////////
// Relayer test suite
/////////////////////////////////////////////////////////////////////////////

func (s *TestSuite) TestLogRelayer(t *C) {
	l := agentLog.NewLogWriter(s.logChan, "test")

	r := agentLog.NewLogRelayer(
		s.mockWsClient,
		s.logChan,
		s.logFile,
		agentLog.LOG_LEVEL_WARN,
	)
	go r.Run()

	l.Debug("debug") // below log level
	l.Info("info") // below log level
	l.Warn("warning msg")
	l.Error("error", "msg")

	got := WaitForLogEntries(s.dataFromClient)
	expect := []agentLog.LogEntry{
		{
			User: "",
			Id: 0,
			Level: 3,
			Service: "test",
			Msg: "warning msg",
		},
		{
			User: "",
			Id: 0,
			Level: 4,
			Service: "test",
			Msg: "error msg",
		},
	}
	t.Check(got, DeepEquals, expect)
}
