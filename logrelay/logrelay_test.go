package logrelay_test

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"strings"
	"testing"
	// Testing
	"github.com/percona/cloud-tools/logrelay"
)

// Hook gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logFile  string
	sendChan chan interface{}
	recvChan chan interface{}
	client   *mock.WebsocketClient
	relay    *logrelay.LogRelay
	logger   *pct.Logger
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.logFile = fmt.Sprintf("/tmp/logrelay_test.go.%d", os.Getpid())

	s.sendChan = make(chan interface{}, 5)
	s.recvChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendChan, s.recvChan)

	s.relay = logrelay.NewLogRelay(s.client, "", proto.LOG_INFO)
	s.logger = pct.NewLogger(s.relay.LogChan(), "test")
	go s.relay.Run() // calls client.Connect()
}

func (s *TestSuite) TearDownSuite(t *C) {
	os.Remove(s.logFile)
}

func (s *TestSuite) SetUpTest(t *C) {
	s.relay.LogLevelChan() <- proto.LOG_INFO

	// Drain the log so one test doesn't affect another.
	_ = test.WaitLog(s.recvChan, 0)
}

func (s *TestSuite) TearDownTest(t *C) {
	// Drain the log so one test doesn't affect another.
	_ = test.WaitLog(s.recvChan, 0)

	s.client.ConnectChan = nil
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
	got := test.WaitLog(s.recvChan, 5)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_DEBUG, Service: "test", Msg: "debug"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Service: "test", Msg: "info"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "test", Msg: "warning"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "error"},
		{Ts: test.Ts, Level: proto.LOG_CRITICAL, Service: "test", Msg: "fatal"},
	}
	t.Check(got, DeepEquals, expect)

	r.LogLevelChan() <- proto.LOG_WARNING
	l.Debug("debug")
	l.Info("info")
	l.Warn("warning")
	l.Error("error")
	l.Fatal("fatal")
	got = test.WaitLog(s.recvChan, 3)
	expect = []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "test", Msg: "warning"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "error"},
		{Ts: test.Ts, Level: proto.LOG_CRITICAL, Service: "test", Msg: "fatal"},
	}
	t.Check(got, DeepEquals, expect)
}

func (s *TestSuite) TestLogFile(t *C) {
	/**
	 * This test is going to be a real pain in the ass because it writes/reads
	 * disk and the disk can be surprisingly slow on a test box.  On top of that,
	 * there's concurrency so we also have to wait for the CPU to run goroutines.
	 */

	r := s.relay
	l := s.logger

	// Online log should work without file log.
	l.Warn("It's a trap!")
	got := test.WaitLog(s.recvChan, 1)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "test", Msg: "It's a trap!"},
	}
	t.Check(got, DeepEquals, expect)

	log, err := ioutil.ReadFile(s.logFile)
	if !os.IsNotExist(err) {
		t.Error("We haven't enabled the log file yet, so it shouldn't exist yet")
	}

	// Enable the log file.
	r.LogFileChan() <- s.logFile

	// Online log should work with the file log.
	l.Warn("It's another trap!")
	got = test.WaitLog(s.recvChan, 1)
	expect = []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "test", Msg: "It's another trap!"},
	}
	t.Check(got, DeepEquals, expect)

	// Log file should exist.
	size, _ := test.FileSize(s.logFile)
	test.WaitFileSize(s.logFile, size)
	log, err = ioutil.ReadFile(s.logFile)
	if err != nil {
		t.Errorf("Log file should exist: %s", err)
		return
	}

	if !strings.Contains(string(log), "It's another trap!") {
		t.Error("Log file should contain only the log entry after being enabled")
	}
	if strings.Contains(string(log), "It's a trap!") {
		t.Error("Log file should contain only the log entry after being enabled")
	}

	l.Debug("Hello")
	if strings.Contains(string(log), "Hello") {
		t.Error("Log file should respect the log level")
	}

	// We should be able to change the log file.
	newLogFile := s.logFile + "-new"
	defer func() { os.Remove(newLogFile) }()
	r.LogFileChan() <- newLogFile
	l.Warn("Foo")

	size, _ = test.FileSize(newLogFile)
	test.WaitFileSize(newLogFile, size)
	log, err = ioutil.ReadFile(newLogFile)
	if err != nil {
		t.Errorf("New log file should exist: %s", err)
		return
	}

	if !strings.Contains(string(log), "Foo") {
		t.Errorf("New log file should contain only the new log entry: %s", string(log))
	}
	if strings.Contains(string(log), "It's another trap!") {
		t.Error("New log file should contain only the new log entry")
	}

	log, err = ioutil.ReadFile(s.logFile)
	if err != nil {
		t.Errorf("Old log file should still exist: %s", err)
		return
	}

	if strings.Contains(string(log), "Foo") {
		t.Error("Old log file should not contain the new log entry")
	}
}

func (s *TestSuite) TestOfflineBuffering(t *C) {
	l := s.logger

	// We're going to cause the relay's client Recv() to get an error
	// which will cause the relay to connect again.  We block this 2nd
	// connect by blocking this chan.  End result: relay remains offline.
	s.client.ConnectChan = make(chan error)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	// Wait for the relay to recv the recv error.
	<-doneChan

	// Wait for the relay to receive its own disconnect notice.
	<-s.client.ConnectChan

	// Relay is offline and trying to connect again in another goroutine.
	// These entries should therefore not be sent.  There's a minor race
	// condition: when relay goes offline, it sends an internal log entry.
	// Sometimes we get that here (Service="logrelay") and sometimes not
	// (len(got)==0).  Either condition is correct for this test.
	l.Error("err1")
	l.Error("err2")
	got := test.WaitLog(s.recvChan, 0)
	if len(got) > 0 && got[0].Service != "logrelay" {
		t.Errorf("Log entries are not sent while offline: %+v", got)
	}

	// Unblock the relay's connect attempt.
	s.client.ConnectChan <- nil

	// Wait for the relay resend what it had ^ buffered.
	got = test.WaitLog(s.recvChan, 3)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: false"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "err1"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "err2"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: true"},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Error(diff)
	}
}

func (s *TestSuite) TestOfflineBufferOverflow(t *C) {
	// Same magic as in TestOfflineBuffering to force relay offline.
	l := s.logger
	s.client.ConnectChan = make(chan error)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	<-doneChan
	<-s.client.ConnectChan
	// Relay is offline.

	// Overflow the first buffer but not the second.  We should get all
	// log entries back.
	for i := 0; i < logrelay.BUFFER_SIZE+1; i++ {
		l.Error(i)
	}

	// Unblock the relay's connect attempt.
	s.client.ConnectChan <- nil

	// Wait for the relay resend what it had ^ buffered.
	// +2 for "connected: false" and "connected: true".
	got := test.WaitLog(s.recvChan, logrelay.BUFFER_SIZE+1+2)
	expect := make([]proto.LogEntry, logrelay.BUFFER_SIZE+1+2)
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: false"}
	for i, n := 0, 1; i < logrelay.BUFFER_SIZE+1; i, n = i+1, n+1 {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("%d", i)}
	}
	expect[logrelay.BUFFER_SIZE+1+1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: true"}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Error(diff)
	}

	// Force the relay offline again, then overflow both buffers. We should get
	// the first buffer, an entry about lost entries (from the second buffer),
	// then the second buffer with the very latest.
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	<-doneChan
	<-s.client.ConnectChan
	// Relay is offline.

	overflow := 3
	for i := 0; i < (logrelay.BUFFER_SIZE*2)+overflow; i++ {
		l.Error(i)
	}

	// Unblock the relay's connect attempt.
	s.client.ConnectChan <- nil

	// +3 for "connected: false", "Lost N entries", and "connected: true".
	got = test.WaitLog(s.recvChan, logrelay.BUFFER_SIZE+overflow+3)
	expect = make([]proto.LogEntry, logrelay.BUFFER_SIZE+overflow+3)
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: false"}
	n := 1
	// If buf size is 10, then we should "Lost connection", get 0-8, a "Lost 10" message for 9-18, then 19-22.
	/**
	 *  /10, first buffer
	 * 1		Lost connection
	 * 2		entry 0
	 * 3		entry 1
	 * 4		entry 2
	 * 5		entry 3
	 * 6		entry 4
	 * 7		entry 5
	 * 8		entry 6
	 * 9		entry 7
	 * 10		entry 8
	 * Entry 9-18 into second buffer, entry 19 causes the overflow and reset:
	 *  /10, second buffer
	 * 1		entry 19
	 * 2		entry 20
	 * 3		entry 21
	 * 4		entry 22
	 */
	for i := 0; i < logrelay.BUFFER_SIZE-1; i++ {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("%d", i)}
		n++
	}
	expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: fmt.Sprintf("Lost %d log entries", logrelay.BUFFER_SIZE)}
	n++
	for i, j := logrelay.BUFFER_SIZE, logrelay.BUFFER_SIZE*2-1; i < logrelay.BUFFER_SIZE+overflow+1; i, j = i+1, j+1 {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("%d", j)}
		n++
	}
	expect[logrelay.BUFFER_SIZE+overflow+2] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "logrelay", Msg: "connected: true"}
	if same, diff := test.IsDeeply(got, expect); !same {
		// @todo: this test is unstable
		t.Error(diff)
	}
}
