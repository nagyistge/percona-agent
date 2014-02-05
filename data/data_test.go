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

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/qan/"

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

func (s *DiskvSpoolerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.dataDir); err != nil {
		t.Error(err)
	}
}

func (s *DiskvSpoolerTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.dataDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *DiskvSpoolerTestSuite) TestSpoolData(t *C) {
	sz := data.NewJsonSerializer()

	// Create and start the spooler.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, sz)
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start()
	if err != nil {
		t.Fatal(err)
	}

	// Doesn't matter what data we spool; just send some bytes...
	ts, _ := time.Parse("2006-01-02 15:04:05", "2013-12-12 15:00:00")
	logEntry := &proto.LogEntry{
		Ts:      ts,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write(logEntry)

	// Spooler should write data to disk, in format of serializer.
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

	gotData, err := spool.Read(gotFiles[0])
	if err != nil {
		t.Error(err)
	}
	t.Assert(string(gotData), Equals, `{"Ts":"2013-12-12T15:00:00Z","Level":1,"Service":"mm","Msg":"hello world"}`)

	spool.Remove(gotFiles[0])

	files = test.WaitFiles(s.dataDir, -1)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}

	spool.Stop()
}

func (s *DiskvSpoolerTestSuite) TestSpoolGzipData(t *C) {
	//go debug(s.logChan)

	sz := data.NewJsonGzipSerializer()

	// See TestSpoolData() for description of these tasks.
	spool := data.NewDiskvSpooler(s.logger, s.dataDir, sz)
	if spool == nil {
		t.Fatal("NewDiskvSpooler")
	}

	err := spool.Start()
	if err != nil {
		t.Fatal(err)
	}

	ts, _ := time.Parse("2006-01-02 15:04:05", "2013-12-12 15:00:00")
	logEntry := &proto.LogEntry{
		Ts:      ts,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	spool.Write(logEntry)

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

	// Decompress and decode and we should have the same LogEntry.
	b := bytes.NewBuffer(gotData)

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
		Ts:      ts,
		Level:   2,
		Service: "mm",
		Msg:     "number 2",
	}
	spool.Write(logEntry2)

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

	b = bytes.NewBuffer(gotData)
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
	tickerChan chan bool
	// --
	client       *mock.WebsocketClient
	sendDataChan chan interface{}
	recvDataChan chan interface{}
}

var _ = Suite(&SenderTestSuite{})

func (s *SenderTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")
	s.tickerChan = make(chan bool, 1)

	s.sendDataChan = make(chan interface{}, 5)
	s.recvDataChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(nil, nil, s.sendDataChan, s.recvDataChan)
	s.client.ErrChan = make(chan error)
	go s.client.Start()
}

func (s *SenderTestSuite) TearDownSuite(t *C) {
}

func (s *SenderTestSuite) SetUpTest(t *C) {
}

func (s *SenderTestSuite) TestSendData(t *C) {
	spool := mock.NewSpooler(nil)

	slow001, err := ioutil.ReadFile(sample + "slow001.json")
	if err != nil {
		t.Fatal(err)
	}

	spool.FilesOut = []string{"slow001.json"}
	spool.DataOut = map[string][]byte{"slow001.json": slow001}

	sender := data.NewSender(s.logger, s.client, "url", spool, s.tickerChan)

	err = sender.Start()
	if err != nil {
		t.Fatal(err)
	}

	data := test.WaitBytes(s.client.RecvBytes)
	if len(data) != 0 {
		t.Errorf("No data sent before tick; got %+v", data)
	}

	s.tickerChan <- true

	data = test.WaitBytes(s.client.RecvBytes)
	if same, diff := test.IsDeeply(data[0], slow001); !same {
		t.Error(diff)
	}

	err = sender.Stop()
	if err != nil {
		t.Fatal(err)
	}
}
