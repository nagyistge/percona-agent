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

package data_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/qan/"

func debug(logChan chan *proto.LogEntry) {
	for logEntry := range logChan {
		log.Println(logEntry)
	}
}

/////////////////////////////////////////////////////////////////////////////
// DiskvSpooler test suite
/////////////////////////////////////////////////////////////////////////////

type DiskvSpoolerTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
	dataDir string
}

var _ = Suite(&DiskvSpoolerTestSuite{})

func (s *DiskvSpoolerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	dir, _ := ioutil.TempDir("/tmp", "pct-data-spooler-test")
	s.dataDir = dir
}

func (s *DiskvSpoolerTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.dataDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *DiskvSpoolerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.dataDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *DiskvSpoolerTestSuite) TestSpoolData(t *C) {
	sz := data.NewJsonSerializer()

	// Create and start the spooler.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, "localhost")
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start(sz)
	if err != nil {
		t.Fatal(err)
	}

	// Doesn't matter what data we spool; just send some bytes...
	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:      now,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write("log", logEntry)

	// Spooler should wrap data in proto.Data and write to disk, in format of serializer.
	files := test.WaitFiles(s.dataDir, 1)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d\n", len(files))
	}

	gotFiles := []string{}
	filesChan := spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}
	if gotFiles[0] != files[0].Name() {
		t.Error("Spool writes and returns " + files[0].Name())
	}
	if len(gotFiles) != len(files) {
		t.Error("Spool writes and returns ", len(files), " file")
	}

	// data is proto.Data[ metadata, Data: proto.LogEntry[...] ]
	data, err := spool.Read(gotFiles[0])
	if err != nil {
		t.Error(err)
	}
	protoData := &proto.Data{}
	if err := json.Unmarshal(data, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "")
	if protoData.Created.IsZero() || protoData.Created.Before(now) {
		// The proto.Data can't be created before the data it contains.
		t.Error("proto.Data.Created after data, got %s", protoData.Created)
	}

	// The LogoEntry we get back should be identical the one we spooled.
	gotLogEntry := &proto.LogEntry{}
	if err := json.Unmarshal(protoData.Data, gotLogEntry); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotLogEntry, logEntry); !same {
		t.Logf("%#v", gotLogEntry)
		t.Error(diff)
	}

	// Removing data from spooler should remove the file.
	spool.Remove(gotFiles[0])
	files = test.WaitFiles(s.dataDir, -1)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}

	spool.Stop()
}

func (s *DiskvSpoolerTestSuite) TestSpoolGzipData(t *C) {
	// Same as TestSpoolData, but use the gzip serializer.

	sz := data.NewJsonGzipSerializer()

	// See TestSpoolData() for description of these tasks.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, "localhost")
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start(sz)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	logEntry := &proto.LogEntry{
		Ts:      now,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write("log", logEntry)

	files := test.WaitFiles(s.dataDir, 1)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d\n", len(files))
	}

	gotFiles := []string{}
	filesChan := spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}

	gotData, err := spool.Read(gotFiles[0])
	if err != nil {
		t.Error(err)
	}
	if len(gotData) <= 0 {
		t.Fatal("1st file has data")
	}

	protoData := &proto.Data{}
	if err := json.Unmarshal(gotData, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "gzip")

	// Decompress and decode and we should have the same LogEntry.
	b := bytes.NewBuffer(protoData.Data)
	g, err := gzip.NewReader(b)
	if err != nil {
		t.Error(err)
	}
	d := json.NewDecoder(g)
	gotLogEntry := &proto.LogEntry{}
	err = d.Decode(gotLogEntry)
	if err := d.Decode(gotLogEntry); err != io.EOF {
		t.Error(err)
	}

	if same, diff := test.IsDeeply(gotLogEntry, logEntry); !same {
		t.Error(diff)
	}

	/**
	 * Do it again to test that serialize is stateless, so to speak.
	 */

	logEntry2 := &proto.LogEntry{
		Ts:      now,
		Level:   2,
		Service: "mm",
		Msg:     "number 2",
	}
	spool.Write("log", logEntry2)

	files = test.WaitFiles(s.dataDir, 2)
	if len(files) != 2 {
		t.Fatalf("Expected 2 file, got %d\n", len(files))
	}

	gotFiles = []string{}
	filesChan = spool.Files()
	for file := range filesChan {
		gotFiles = append(gotFiles, file)
	}

	gotData, err = spool.Read(gotFiles[1]) // 2nd data, 2nd file
	if err != nil {
		t.Error(err)
	}
	if len(gotData) <= 0 {
		t.Fatal("2nd file has data")
	}

	protoData = &proto.Data{}
	if err := json.Unmarshal(gotData, protoData); err != nil {
		t.Fatal(err)
	}
	t.Check(protoData.Service, Equals, "log")
	t.Check(protoData.ContentType, Equals, "application/json")
	t.Check(protoData.ContentEncoding, Equals, "gzip")

	b = bytes.NewBuffer(protoData.Data)
	g, err = gzip.NewReader(b)
	if err != nil {
		t.Error(err)
	}
	d = json.NewDecoder(g)
	gotLogEntry = &proto.LogEntry{}
	err = d.Decode(gotLogEntry)
	if err := d.Decode(gotLogEntry); err != io.EOF {
		t.Error(err)
	}

	if same, diff := test.IsDeeply(gotLogEntry, logEntry2); !same {
		t.Error(diff)
	}

	spool.Stop()
}

/////////////////////////////////////////////////////////////////////////////
// Sender test suite
/////////////////////////////////////////////////////////////////////////////

type SenderTestSuite struct {
	logChan    chan *proto.LogEntry
	logger     *pct.Logger
	tickerChan chan time.Time
	// --
	dataChan chan []byte
	respChan chan interface{}
	client   *mock.DataClient
}

var _ = Suite(&SenderTestSuite{})

func (s *SenderTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")
	s.tickerChan = make(chan time.Time, 1)

	s.dataChan = make(chan []byte, 5)
	s.respChan = make(chan interface{})
	s.client = mock.NewDataClient(s.dataChan, s.respChan)
}

// --------------------------------------------------------------------------

func (s *SenderTestSuite) TestSendData(t *C) {
	spool := mock.NewSpooler(nil)

	slow001, err := ioutil.ReadFile(sample + "slow001.json")
	if err != nil {
		t.Fatal(err)
	}

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": slow001}

	sender := data.NewSender(s.logger, s.client)

	err = sender.Start(spool, s.tickerChan, false)
	if err != nil {
		t.Fatal(err)
	}

	data := test.WaitBytes(s.dataChan)
	if len(data) != 0 {
		t.Errorf("No data sent before tick; got %+v", data)
	}

	s.tickerChan <- time.Now()

	data = test.WaitBytes(s.dataChan)
	if same, diff := test.IsDeeply(data[0], slow001); !same {
		t.Error(diff)
	}

	t.Check(len(spool.DataOut), Equals, 1)

	select {
	case s.respChan <- &proto.Response{Code: 200}:
	case <-time.After(500 * time.Millisecond):
		t.Error("Sender receives prot.Response after sending data")
	}

	// Sender should include its websocket client status.  We're using a mock ws client
	// which reports itself as "data-client: ok".
	status := sender.Status()
	t.Check(status["data-client"], Equals, "ok")

	err = sender.Stop()
	t.Assert(err, IsNil)

	t.Check(len(spool.DataOut), Equals, 0)
}

func (s *SenderTestSuite) TestBlackhole(t *C) {
	spool := mock.NewSpooler(nil)

	slow001, err := ioutil.ReadFile(sample + "slow001.json")
	if err != nil {
		t.Fatal(err)
	}

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": slow001}

	sender := data.NewSender(s.logger, s.client)

	err = sender.Start(spool, s.tickerChan, true) // <- true = enable blackhole
	if err != nil {
		t.Fatal(err)
	}

	s.tickerChan <- time.Now()

	data := test.WaitBytes(s.dataChan)
	if len(data) != 0 {
		t.Errorf("Data sent despite blackhole; got %+v", data)
	}

	select {
	case s.respChan <- &proto.Response{Code: 200}:
		// Should not recv response because no data was sent.
		t.Error("Sender receives prot.Response after sending data")
	case <-time.After(500 * time.Millisecond):
	}

	err = sender.Stop()
	t.Assert(err, IsNil)
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan  chan *proto.LogEntry
	logger   *pct.Logger
	tmpDir   string
	dataDir  string
	dataChan chan []byte
	respChan chan interface{}
	client   *mock.DataClient
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.dataDir = pct.Basedir.Dir("data")

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	dir, _ := ioutil.TempDir("/tmp", "pct-data-spooler-test")
	s.dataDir = dir

	s.dataChan = make(chan []byte, 5)
	s.respChan = make(chan interface{})
	s.client = mock.NewDataClient(s.dataChan, s.respChan)
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	m := data.NewManager(s.logger, s.dataDir, "localhost", s.client)
	t.Assert(m, NotNil)

	config := &data.Config{
		Encoding:     "",
		SendInterval: 1,
	}
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)

	err = m.Start(&proto.Cmd{}, configData)
	t.Assert(err, IsNil)

	sender := m.Sender()
	t.Check(sender, NotNil)

	/**
	 * GetConfig
	 */

	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}

	gotReply := m.Handle(cmd)
	expectReply := cmd.Reply(config)
	if same, diff := test.IsDeeply(gotReply, expectReply); !same {
		t.Logf("%+v", gotReply)
		t.Error(diff)
	}

	cmd.Cmd = "StopService"
	err = m.Stop(cmd)
	t.Assert(err, IsNil)
	if !test.WaitStatus(5, m, "data", "Stopped") {
		t.Fatal("test.WaitStatus() timeout")
	}
	status := m.Status()
	t.Check(status["data-spooler"], Equals, "Stopped")
	t.Check(status["data-sender"], Equals, "Stopped")
}

func (s *ManagerTestSuite) TestSetConfig(t *C) {
	m := data.NewManager(s.logger, s.dataDir, "localhost", s.client)
	t.Assert(m, NotNil)

	config := &data.Config{
		Encoding:     "",
		SendInterval: 1,
	}
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)

	err = m.Start(&proto.Cmd{}, configData)
	t.Assert(err, IsNil)

	sender := m.Sender()
	t.Check(sender, NotNil)

	/**
	 * Change SendInterval
	 */
	config.SendInterval = 5
	configData, err = json.Marshal(config)
	t.Assert(err, IsNil)
	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "SetConfig",
		Data:    configData,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply.Error, Equals, "")

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}
	gotReply = m.Handle(cmd)
	gotNewConfig := &data.Config{}
	err = json.Unmarshal(gotReply.Data, gotNewConfig)
	t.Assert(err, IsNil)
	t.Check(gotNewConfig.SendInterval, Equals, 5)
	if same, diff := test.IsDeeply(gotNewConfig, config); !same {
		test.Dump(gotNewConfig)
		t.Error(diff)
	}

	// Verify new config on disk.
	content, err := ioutil.ReadFile(pct.Basedir.ConfigFile("data"))
	t.Assert(err, IsNil)
	gotConfig := &data.Config{}
	if err := json.Unmarshal(content, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotConfig, config); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}

	/**
	 * Change Encoding
	 */
	config.Encoding = "gzip"
	configData, err = json.Marshal(config)
	t.Assert(err, IsNil)
	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "SetConfig",
		Data:    configData,
	}

	gotReply = m.Handle(cmd)
	t.Assert(gotReply.Error, Equals, "")

	cmd = &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "GetConfig",
	}
	gotReply = m.Handle(cmd)
	gotNewConfig = &data.Config{}
	err = json.Unmarshal(gotReply.Data, gotNewConfig)
	t.Assert(err, IsNil)
	t.Check(gotNewConfig.Encoding, Equals, "gzip")
	if same, diff := test.IsDeeply(gotNewConfig, config); !same {
		test.Dump(gotNewConfig)
		t.Error(diff)
	}

	// Verify new config on disk.
	content, err = ioutil.ReadFile(pct.Basedir.ConfigFile("data"))
	t.Assert(err, IsNil)
	gotConfig = &data.Config{}
	if err := json.Unmarshal(content, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotConfig, config); !same {
		test.Dump(gotConfig)
		t.Error(diff)
	}
}

func (s *ManagerTestSuite) TestStatus(t *C) {
	// Start a data manager.
	m := data.NewManager(s.logger, s.dataDir, "localhost", s.client)
	t.Assert(m, NotNil)
	config := &data.Config{
		Encoding:     "gzip",
		SendInterval: 1,
	}
	configData, err := json.Marshal(config)
	t.Assert(err, IsNil)
	err = m.Start(&proto.Cmd{}, configData)
	t.Assert(err, IsNil)

	// Get its status directly.
	if !test.WaitStatus(5, m, "data", "Ready") {
		t.Fatal("test.WaitStatus() timeout")
	}
	status := m.Status()
	t.Check(status["data"], Equals, "Ready")
	t.Check(status["data-spooler"], Equals, "Idle")
	t.Check(status["data-sender"], Equals, "Idle")

	// Get its status via cmd.
	cmd := &proto.Cmd{
		User:    "daniel",
		Service: "data",
		Cmd:     "Status",
	}
	gotReply := m.Handle(cmd)
	gotStatus := make(map[string]string)
	if err := json.Unmarshal(gotReply.Data, &gotStatus); err != nil {
		t.Fatal(err)
	}
	t.Check(gotStatus["data"], Equals, "Ready")
	t.Check(gotStatus["data-spooler"], Equals, "Idle")
	t.Check(gotStatus["data-sender"], Equals, "Idle")
}
