/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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
	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/log"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
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

func (s *RelayTestSuite) SetUpTest(t *C) {
	s.relay.LogLevelChan() <- proto.LOG_INFO
	s.client.SetConnectChan(nil)

	test.DrainLogChan(s.logChan)
	test.DrainRecvData(s.recvChan)

	if !test.WaitStatus(3, s.relay, "log-buf1", "0") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("First buffer full")
	}

	if !test.WaitStatus(3, s.relay, "log-buf2", "0") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("Second buffer has overflow")
	}

	test.DrainTraceChan(s.client.TraceChan)
}

func (s *RelayTestSuite) TearDownSuite(t *C) {
	os.Remove(s.logFile)
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
		{Ts: test.Ts, Level: proto.LOG_DEBUG, Tool: "test", Msg: "debug"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "test", Msg: "info"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "test", Msg: "warning"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: "error"},
		{Ts: test.Ts, Level: proto.LOG_CRITICAL, Tool: "test", Msg: "fatal"},
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
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "test", Msg: "warning"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: "error"},
		{Ts: test.Ts, Level: proto.LOG_CRITICAL, Tool: "test", Msg: "fatal"},
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
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "test", Msg: "It's a trap!"},
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
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "test", Msg: "It's another trap!"},
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

	// We're going to cause the relay's client Send() to get an error
	// which will cause the relay to connect again.  We block this 2nd
	// connect by blocking the connectChan.  End result: relay is offline.
	s.client.SetConnectChan(s.connectChan)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.SendError <- io.EOF
		doneChan <- true
	}()

	// Send a log entry so to cause the fake error ^
	l.Info("I get the Send error")

	// Wait for the relay to recv the error.
	<-doneChan

	// Wait for the relay to disconnect then call client.Connect().
	<-s.connectChan

	// Double-check that relay is offline.
	if !test.WaitStatus(1, s.relay, "ws", "Disconnected") {
		t.Fatal("Relay connects")
	}

	// Relay is offline and trying to connect again in another goroutine.
	// These entries should therefore not be sent.  There's a minor race
	// condition: when relay goes offline, it sends an internal log entry.
	// Sometimes we get that here (Service="log") and sometimes not
	// (len(got)==0).  Either condition is correct for this test.
	l.Error("err1")
	l.Error("err2")
	got := test.WaitLog(s.recvChan, -1)
	if len(got) > 0 && got[0].Tool != "log" {
		t.Errorf("Log entries are not sent while offline: %+v", got)
	}

	// Unblock the relay's connect attempt.
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "ws", "Connected") {
		t.Fatal("Relay connects")
	}

	// Wait for the relay resend what it had ^ buffered.
	got = test.WaitLog(s.recvChan, 5)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "test", Msg: "I get the Send error"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log", Msg: "Lost connection to API"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: "err1"},
		{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: "err2"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Connected to API"},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

func (s *RelayTestSuite) TestOffline1stBufferOverflow(t *C) {
	// Same magic as in TestOfflineBuffering to force relay offline.
	l := s.logger
	s.client.SetConnectChan(s.connectChan)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.SendError <- io.EOF
		doneChan <- true
	}()
	l.Info("I get the Send error")
	<-doneChan
	<-s.connectChan
	// Relay is offline, trying to connect.

	// Overflow the first buffer but not the second.  We should get all
	// log entries back.  We overflow it by 4 entries:
	// +1			I get the Send error
	// +2			Lost connection to API
	// buf size		a:n (loop below)
	// +3			a:n (loop below)
	// +4			Connected to API
	for i := 1; i <= log.BUFFER_SIZE+1; i++ {
		l.Error(fmt.Sprintf("a:%d", i))
	}

	// Wait until the first buf is full.
	if !test.WaitStatus(3, s.relay, "log-buf1", fmt.Sprintf("%d", log.BUFFER_SIZE)) {
		status := s.relay.Status()
		t.Log(status)
		t.Error("First buffer full")
	}

	// Wait until the second buf has the overflow which is 3 not 4 here because
	// "Connected to API" won't be logged until the next block...
	if !test.WaitStatus(3, s.relay, "log-buf2", "3") {
		status := s.relay.Status()
		t.Log(status)
		t.Error("Second buffer has overflow")
	}

	// Unblock the relay's connect attempt.  This causes "Connected to API" to
	// be log (+4 overflow).
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "ws", "Connected") {
		t.Fatal("Relay connects")
	}

	// Wait for the relay resend what it had ^ buffered.
	got := test.WaitLog(s.recvChan, log.BUFFER_SIZE+4)

	// Check that we still get all log entries.
	expect := make([]proto.LogEntry, log.BUFFER_SIZE+4)
	// First two msgs (+1 and +2):
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "test", Msg: "I get the Send error"}
	expect[1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log", Msg: "Lost connection to API"}
	// The overflow (buf size and +3):
	for i, n := 1, 2; i <= log.BUFFER_SIZE+1; i, n = i+1, n+1 {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: fmt.Sprintf("a:%d", i)}
	}
	// Last msg (+4):
	expect[len(expect)-1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Connected to API"}
	if same, diff := test.IsDeeply(got, expect); !same {
		t.Error(diff)
	}

	// Both bufs should be empty now.
	if !test.WaitStatus(2, s.relay, "log-buf1", "0") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("1st buf empty")
	}
	if !test.WaitStatus(2, s.relay, "log-buf2", "0") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("2nd buf empty")
	}
}

func (s *RelayTestSuite) TestOffline2ndBufferOverflow(t *C) {
	// This test is like TestOffline1stBufferOverflow but now we'll overflow
	// the 2nd buf too which causes us to lose some log entries and get a
	// "Lost N log entries" warning.

	// Same magic as in TestOfflineBuffering to force relay offline.
	l := s.logger
	s.client.SetConnectChan(s.connectChan)
	doneChan := make(chan bool, 1)
	go func() {
		s.client.SendError <- io.EOF
		doneChan <- true
	}()
	l.Info("I get the Send error")
	<-doneChan
	<-s.connectChan
	// Relay is offline, trying to connect.

	// Overflow the 1st and 2nd buffs.  Note: there's already "I get the Send error" (+1)
	// and "Lost connection to API" (+2) in the 1st buf.  The +1 here makes a +3 overflow.
	for i := 1; i <= (log.BUFFER_SIZE*2)+1; i++ {
		l.Error(fmt.Sprintf("b:%d", i))
	}

	// Wait until the first buf is full.
	if !test.WaitStatus(3, s.relay, "log-buf1", fmt.Sprintf("%d", log.BUFFER_SIZE)) {
		status := s.relay.Status()
		t.Log(status)
		t.Error("First buffer full")
	}

	// For for the 2nd buf to be full.
	if !test.WaitStatus(3, s.relay, "log-buf2", "3") {
		status := s.relay.Status()
		t.Log(status)
		t.Fatal("2nd buf full")
	}

	// Unblock the relay's connect attempt.  This adds "Connected to API" (+4).
	s.connectChan <- true
	if !test.WaitStatus(1, s.relay, "ws", "Connected") {
		t.Fatal("Relay connects")
	}

	nSum := log.BUFFER_SIZE + 5
	got := test.WaitLog(s.recvChan, nSum)
	if len(got) < nSum {
		status := s.relay.Status()
		t.Log(status)
		t.Fatalf("Expected %d log entrires, got %d", nSum, len(got))
	}

	// When the 2nd buf overflows, it's reset and the overflow msg becomes buf2[0].
	// After re-connecting, a "Lost N log entries" msg is sent (+5).  So if buf size
	// is 10, then we should get:
	/**
	 * buf1:
	 *  1		I get the Send error (+1)
	 *  2		Lost connection to API (+2)
	 *  3		entry 1
	 *  4		entry 2
	 *  5		entry 3
	 *  6		entry 4
	 *  7		entry 5
	 *  8		entry 6
	 *  9		entry 7
	 * 10		entry 8
	 * ---		Lost 10 log entries (+5)
	 * 9-10 and 11-18 go into buf2, 19 overlows and resets buf2:
	 * buf2:
	 *  1		entry 19
	 *  2		entry 20
	 *  3       entry 21 (+3)
	 * ---		Connected to API (+4)
	 */
	expect := make([]proto.LogEntry, nSum)
	expect[0] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "test", Msg: "I get the Send error"}
	expect[1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log", Msg: "Lost connection to API"}
	n := 2
	// entries 1-8 (buf size 10 - 2 = 8)
	for i := 1; i <= log.BUFFER_SIZE-2; i++ {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: fmt.Sprintf("b:%d", i)}
		n++
	}
	// n=10 (buf2[0] if buf size = 10)
	// This is logged after resending buf1 and before resending buf2:
	expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log", Msg: fmt.Sprintf("Lost %d log entries", log.BUFFER_SIZE)}
	n++
	// buf2:
	for i := log.BUFFER_SIZE*2 - 1; n < len(expect)-1; i++ {
		expect[n] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_ERROR, Tool: "test", Msg: fmt.Sprintf("b:%d", i)}
		n++
	}
	// Last msg (+4):
	expect[len(expect)-1] = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Connected to API"}

	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	tmpDir      string
	sendChan    chan interface{}
	recvChan    chan interface{}
	connectChan chan bool
	client      *mock.WebsocketClient
	logChan     chan *proto.LogEntry
	logFile     string
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}

	s.sendChan = make(chan interface{}, log.BUFFER_SIZE*3)
	s.recvChan = make(chan interface{}, log.BUFFER_SIZE*3)
	s.connectChan = make(chan bool)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendChan, s.recvChan)
	s.logChan = make(chan *proto.LogEntry, log.BUFFER_SIZE*3)
	s.logFile = s.tmpDir + "/log"
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestLogService(t *C) {
	config := &log.Config{
		File:  s.logFile,
		Level: "info",
	}
	pct.Basedir.WriteConfig("log", config)

	m := log.NewManager(s.client, s.logChan)
	err := m.Start()
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
		if l.Tool == "log-svc-test" {
			gotLog = l
			break
		}
	}
	t.Assert(gotLog, NotNil)
	expectLog := proto.LogEntry{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log-svc-test", Msg: "i'm a log entry"}
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
	err = m.Stop()
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
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		User:    "daniel",
		Tool: "log",
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
		if l.Tool == "log-svc-test" {
			gotLog = l
			break
		}
	}
	expectLog = proto.LogEntry{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log-svc-test", Msg: "blah"}
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

	// Verify new log config on disk.
	data, err := ioutil.ReadFile(pct.Basedir.ConfigFile("log"))
	t.Assert(err, IsNil)
	gotConfig := &log.Config{}
	if err := json.Unmarshal(data, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotConfig, config); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}

	/**
	 * GetConfig
	 */

	cmd = &proto.Cmd{
		User:    "daniel",
		Tool: "log",
		Cmd:     "GetConfig",
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")
	t.Assert(reply.Data, NotNil)
	gotConfigRes := []proto.AgentConfig{}
	if err := json.Unmarshal(reply.Data, &gotConfigRes); err != nil {
		t.Fatal(err)
	}
	expectConfigRes := []proto.AgentConfig{
		{
			Tool: "log",
			Config:          string(configData),
			Running:         true,
		},
	}
	if same, diff := test.IsDeeply(gotConfigRes, expectConfigRes); !same {
		test.Dump(gotConfigRes)
		t.Error(diff)
	}

	/**
	 * Status (internal status of log and relay)
	 */

	status := m.Status()
	t.Check(status["ws"], Equals, "Connected")
	t.Check(status["log-file"], Equals, newLogFile)
	t.Check(status["log-level"], Equals, "warning")
}

func (s *ManagerTestSuite) TestReconnect(t *C) {
	config := &log.Config{
		File:  s.logFile,
		Level: "info",
	}
	pct.Basedir.WriteConfig("log", config)

	m := log.NewManager(s.client, s.logChan)
	err := m.Start()
	t.Assert(err, IsNil)

	// Wait for relay to start and connect.
	got := test.WaitLog(s.recvChan, 2)
	expect := []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Started"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Connected to API"},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Fatal(diff)
	}

	relay := m.Relay()
	t.Assert(relay, NotNil)

	logger := pct.NewLogger(relay.LogChan(), "log-svc-test")
	logger.Info("before reconnect")

	s.client.SetConnectChan(s.connectChan)

	cmd := &proto.Cmd{
		User:    "daniel",
		Tool: "log",
		Cmd:     "Reconnect",
	}
	reply := m.Handle(cmd)
	t.Check(reply.Error, Equals, "")

	// Wait for relay to reconnect.
	select {
	case <-s.connectChan:
	case <-time.After(2 * time.Second):
		t.Fatal("Relay did not reconnect")
	}

	// Let relay reconnect
	s.connectChan <- true
	if !test.WaitStatus(1, relay, "ws", "Connected") {
		t.Fatal("Relay connects")
	}

	logger.Info("after reconnect")

	got = test.WaitLog(s.recvChan, 4)
	expect = []proto.LogEntry{
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log-svc-test", Msg: "before reconnect"},
		{Ts: test.Ts, Level: proto.LOG_WARNING, Tool: "log", Msg: "Lost connection to API"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log", Msg: "Connected to API"},
		{Ts: test.Ts, Level: proto.LOG_INFO, Tool: "log-svc-test", Msg: "after reconnect"},
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}
