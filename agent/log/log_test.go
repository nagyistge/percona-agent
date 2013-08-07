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
	fromClients chan *proto.Msg
	toClients chan *proto.Msg
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

	s.fromClients = make(chan *proto.Msg, 10)
	s.toClients = make(chan *proto.Msg, 10)
	s.mockWsClient = ws_client.NewMockClient(s.fromClients, s.toClients)

	s.logChan = make(chan *agentLog.LogEntry, 10)
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
			Entry: "debug",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Info("info")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 2,
			Service: "test",
			Entry: "info",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Warn("warning")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 3,
			Service: "test",
			Entry: "warning",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Error("error")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 4,
			Service: "test",
			Entry: "error",
		},
	}
	t.Check(got, DeepEquals, expect)

	l.Fatal("fatal")
	got = getLogEntries(s)
	expect = []agentLog.LogEntry{
		{
			Level: 5,
			Service: "test",
			Entry: "fatal",
		},
	}
	t.Check(got, DeepEquals, expect)
}

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
	l.Warn("warning")
	l.Error("error")

	got := WaitForClientMsgs(s.fromClients)
	expect := []proto.Msg{
		{Cmd:"log", Data:`{"level":3,"service":"test","entry":"warning"}`},
		{Cmd:"log", Data:`{"level":4,"service":"test","entry":"error"}`},
	}
	t.Check(got, DeepEquals, expect)
}
