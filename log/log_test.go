/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package log_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/log"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"strings"
	"testing"
)

// Hook gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Relay test suite
/////////////////////////////////////////////////////////////////////////////

type RelayTestSuite struct {
	logChan     chan *proto.LogEntry
	logFile     string
	sendChan    chan interface{}
	recvChan    chan interface{}
	connectChan chan bool
	client      *mock.WebsocketClient
	relay       *log.Relay
	logger      *pct.Logger
}

var _ = Suite(&RelayTestSuite{})

func (s *RelayTestSuite) SetUpSuite(t *C) {
	s.logFile = fmt.Sprintf("/tmp/log_test.go.%d", os.Getpid())

	s.sendChan = make(chan interface{}, 5)
	s.recvChan = make(chan interface{}, 5)
	s.connectChan = make(chan bool)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendChan, s.recvChan)

	s.logChan = make(chan *proto.LogEntry, log.BUFFER_SIZE*3)
	s.relay = log.NewRelay(s.client, s.logChan, "", proto.LOG_INFO, false)
	s.logger = pct.NewLogger(s.relay.LogChan(), "test")
	go s.relay.Run() // calls client.Connect()
}

func (s *RelayTestSuite) TearDownSuite(t *C) {
	os.Remove(s.logFile)
}

func (s *RelayTestSuite) SetUpTest(t *C) {
	s.relay.LogLevelChan() <- proto.LOG_INFO

	// Drain the log so one test doesn't affect another.
	_ = test.WaitLog(s.recvChan, 0)
}

func (s *RelayTestSuite) TearDownTest(t *C) {
	// Drain the log so one test doesn't affect another.
	_ = test.WaitLog(s.recvChan, 0)

	s.client.SetConnectChan(nil)
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *RelayTestSuite) TestLogLevel(t *C) {
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

func (s *RelayTestSuite) TestLogFile(t *C) {
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
		t.Fatalf("Log file should exist: %s", err)
	}

	if !strings.Contains(string(log), "It's another trap!") {
		t.Error("Log file contains entry after being enabled, got\n", string(log))
	}
	if strings.Contains(string(log), "It's a trap!") {
		t.Error("Log file does not contain entry before being enabled, got\n", string(log))
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
		t.Error("New log file contains only the new log entry, got\n", string(log))
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

func (s *RelayTestSuite) TestOfflineBuffering(t *C) {
	l := s.logger

	// We're going to cause the relay's client Recv() to get an error
	// which will cause the relay to connect again.  We block this 2nd
	// connect by blocking this chan.  End result: relay remains offline.
	s.client.SetConnectChan(s.connectChan)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	// Wait for the relay to recv the recv error.
	<-doneChan

	// Wait for the relay to call client.Connect().
	<-s.connectChan

	// Relay is offline and trying to connect again in another goroutine.
	// These entries should therefore not be sent.  There's a minor race
	// condition: when relay goes offline, it sends an internal log entry.
	// Sometimes we get that here (Service="log") and sometimes not
	// (len(got)==0).  Either condition is correct for this test.
	l.Error("err1")
	l.Error("err2")
	got := test.WaitLog(s.recvChan, 0)
	if len(got) > 0 && got[0].Service != "log" {
		t.Errorf("Log entries are not sent while offline: %+v", got)
	}

	// Unblock the relay's connect attempt.
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "log-api", "Connected") {
		t.Fatal("Relay connects")
	}

	// Wait for the relay resend what it had ^ buffered.
	got = test.WaitLog(s.recvChan, 3)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: false"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "err1"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: "err2"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: true"},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Error(diff)
	}
}

func (s *RelayTestSuite) TestOfflineBufferOverflow(t *C) {
	// Same magic as in TestOfflineBuffering to force relay offline.
	l := s.logger
	s.client.SetConnectChan(s.connectChan)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	<-doneChan
	<-s.connectChan
	// Relay is offline, trying to connect.

	// Overflow the first buffer but not the second.  We should get all
	// log entries back.
	for i := 0; i < log.BUFFER_SIZE+1; i++ {
		l.Error(fmt.Sprintf("a:%d", i))
	}
	if !test.WaitStatus(3, s.relay, "log-buf1", fmt.Sprintf("%d", log.BUFFER_SIZE)) {
		t.Error("First buffer full")
	}

	// Unblock the relay's connect attempt.
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "log-api", "Connected") {
		t.Fatal("Relay connects")
	}

	// Wait for the relay resend what it had ^ buffered.
	// +2 for "connected: false" and "connected: true".
	got := test.WaitLog(s.recvChan, log.BUFFER_SIZE+1+2)

	expect := make([]proto.LogEntry, log.BUFFER_SIZE+1+2)
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: false"}
	for i, n := 0, 1; i < log.BUFFER_SIZE+1; i, n = i+1, n+1 {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("a:%d", i)}
	}
	expect[log.BUFFER_SIZE+1+1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: true"}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Error(diff)
	}

	if !test.WaitStatus(3, s.relay, "log-buf2", "0") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("1st buf empty")
	}
	expect = []proto.LogEntry{}

	// Force the relay offline again, then overflow both buffers. We should get
	// the first buffer, an entry about lost entries (from the second buffer),
	// then the second buffer with the very latest.
	go func() {
		s.client.RecvError <- io.EOF
		doneChan <- true
	}()
	<-doneChan
	<-s.connectChan
	// Relay is offline, trying to connect.

	overflow := 3
	for i := 0; i < (log.BUFFER_SIZE*2)+overflow; i++ {
		l.Error(fmt.Sprintf("b:%d", i))
	}
	if !test.WaitStatus(3, s.relay, "log-buf2", fmt.Sprintf("%d", overflow+1)) {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("2nd buf full")
	}

	// Unblock the relay's connect attempt.
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "log-api", "Connected") {
		t.Fatal("Relay connects")
	}

	// +3 for "connected: false", "Lost N entries", and "connected: true".
	got = test.WaitLog(s.recvChan, log.BUFFER_SIZE+overflow+3)

	expect = make([]proto.LogEntry, log.BUFFER_SIZE+overflow+3)
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: false"}
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
	for i := 0; i < log.BUFFER_SIZE-1; i++ {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("b:%d", i)}
		n++
	}
	expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: fmt.Sprintf("Lost %d log entries", log.BUFFER_SIZE)}
	n++
	for i, j := log.BUFFER_SIZE, log.BUFFER_SIZE*2-1; i < log.BUFFER_SIZE+overflow+1; i, j = i+1, j+1 {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Service: "test", Msg: fmt.Sprintf("b:%d", j)}
		n++
	}
	expect[log.BUFFER_SIZE+overflow+2] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log", Msg: "connected: true"}
	if same, diff := test.IsDeeply(got, expect); !same {
		// @todo: this test may still be unstable
		n := len(got)
		if len(expect) > n {
			n = len(expect)
		}
		for i := 0; i < n; i++ {
			var gotL proto.LogEntry
			var expectL proto.LogEntry
			if i < len(got) {
				gotL = got[i]
			}
			if i < len(expect) {
				expectL = expect[i]
			}
			t.Logf("%+v %+v\n", gotL, expectL)
		}
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	sendChan    chan interface{}
	recvChan    chan interface{}
	connectChan chan bool
	client      *mock.WebsocketClient
	logChan     chan *proto.LogEntry
	logFile     string
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.sendChan = make(chan interface{}, 5)
	s.recvChan = make(chan interface{}, 5)
	s.connectChan = make(chan bool)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendChan, s.recvChan)
	s.logChan = make(chan *proto.LogEntry, log.BUFFER_SIZE*3)
	s.logFile = fmt.Sprintf("/tmp/log_test.go.%d", os.Getpid())
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	os.Remove(s.logFile)
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestLogService(t *C) {
	config := &log.Config{
		File:  s.logFile,
		Level: "info",
	}
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)

	m := log.NewManager(s.client, s.logChan)
	err = m.Start(&proto.Cmd{}, configData)
	t.Assert(err, IsNil)

	relay := m.Relay()
	t.Assert(relay, NotNil)

	logger := pct.NewLogger(relay.LogChan(), "log-svc-test")
	logger.Info("i'm a log entry")

	// Log entry should be sent to API.
	got := test.WaitLog(s.recvChan, 3)
	if len(got) == 0 {
		t.Fatal("No log entries")
	}
	var gotLog proto.LogEntry
	for _, l := range got {
		if l.Service == "log-svc-test" {
			gotLog = l
			break
		}
	}
	t.Assert(gotLog, NotNil)
	expectLog := proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Service: "log-svc-test", Msg: "i'm a log entry"}
	if same, diff := test.IsDeeply(gotLog, expectLog); !same {
		t.Logf("%+v", got)
		t.Error(diff)
	}

	// Since there's a log file, entry should be written to it too.
	size, _ := test.FileSize(s.logFile)
	test.WaitFileSize(s.logFile, size)
	var content []byte
	content, err = ioutil.ReadFile(s.logFile)
	t.Assert(err, IsNil)

	if !strings.Contains(string(content), "i'm a log entry") {
		t.Error("Writes log entry to log file, got\n", string(content))
	}

	// Can't stop log service, but Stop() should work and not return error.
	err = m.Stop(&proto.Cmd{})
	t.Assert(err, IsNil)

	/**
	 * Change log level and file
	 */

	newLogFile := s.logFile + "-2"
	defer os.Remove(newLogFile)

	config = &log.Config{
		File:  newLogFile,
		Level: "warning",
	}
	configData, err = json.Marshal(config)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "log",
		Cmd:     "SetConfig",
		Data:    configData,
	}

	gotReply := m.Handle(cmd)
	expectReply := cmd.Reply(config)
	if same, diff := test.IsDeeply(gotReply, expectReply); !same {
		t.Logf("%+v", gotReply)
		t.Error(diff)
	}

	// Log entry should NOT be sent to API if log level was really changed.
	logger.Info("i'm lost")
	got = test.WaitLog(s.recvChan, 3)
	if len(got) != 0 {
		t.Logf("%+v", got)
		t.Error("Log level changed dynamically")
	}

	logger.Warn("blah")
	got = test.WaitLog(s.recvChan, 3)
	gotLog = proto.LogEntry{}
	for _, l := range got {
		if l.Service == "log-svc-test" {
			gotLog = l
			break
		}
	}
	expectLog = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Service: "log-svc-test", Msg: "blah"}
	if same, diff := test.IsDeeply(gotLog, expectLog); !same {
		t.Logf("%+v", got)
		t.Error(diff)
	}

	// Entry should be written to new log file if it was really changed.
	size, _ = test.FileSize(newLogFile)
	test.WaitFileSize(newLogFile, size)
	content, err = ioutil.ReadFile(newLogFile)
	t.Assert(err, IsNil)
	if !strings.Contains(string(content), "blah") {
		t.Error("Log file changed dynamically, got\n", string(content))
	}

	/**
	 * GetConfig
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "log",
		Cmd:     "GetConfig",
	}

	gotReply = m.Handle(cmd)
	expectReply = cmd.Reply(config)
	if same, diff := test.IsDeeply(gotReply, expectReply); !same {
		t.Logf("%+v", gotReply)
		t.Error(diff)
	}

	/**
	 * Status (internal status of log and relay)
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "log",
		Cmd:     "Status",
	}

	gotReply = m.Handle(cmd)
	status := make(map[string]string)
	if err := json.Unmarshal(gotReply.Data, &status); err != nil {
		t.Error(err)
	}
	t.Check(status["log-api"], Equals, "Connected")
	t.Check(status["log-file"], Equals, newLogFile)
	t.Check(status["log-level"], Equals, "warning")
}
