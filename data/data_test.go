package data_test

import (
	"fmt"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-protocol/test/mock"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/test"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"testing"
	"time"
	// Testing
	"github.com/percona/cloud-tools/data"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logChan    chan *proto.LogEntry
	logger     *pct.Logger
	postChan   chan []byte
	client     pct.HttpClient
	dataChan   chan interface{}
	dataDir    string
	tickerChan chan bool
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	s.postChan = make(chan []byte, 2)
	s.client = &mock.HttpClient{PostChan: s.postChan}

	s.dataChan = make(chan interface{}, 3)

	dir, _ := ioutil.TempDir("/tmp", "pct-data-sender")
	s.dataDir = dir

	s.tickerChan = make(chan bool, 1)
}

func (s *TestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.dataDir); err != nil {
		fmt.Println(err)
	}
}

func (s *TestSuite) SetUpTeset(t *C) {
	if s.tickerChan == nil {
		s.tickerChan = make(chan bool, 1)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
/////////////////////////////////////////////////////////////////////////////

func (s *TestSuite) TestSpoolAndSend(t *C) {
	// Create the data sender
	dataSender := data.NewSender(s.logger, s.client, s.dataChan, s.dataDir, s.tickerChan)
	t.Assert(dataSender, NotNil)

	// Run the data sender: one gorutine receives data on dataChan and writes
	// it to disk, so let's test that first...
	dataSender.Start()

	// Doesn't matter what data we send; just send some bytes...
	ts, _ := time.Parse("2006-01-02 15:04:05", "2013-12-12 15:00:00")
	logEntry := &proto.LogEntry{
		Ts:      ts,
		Level:   1,
		Service: "mm",
		Msg:     "hello world",
	}
	s.dataChan <- logEntry

	// Sender should receive the data and write it to disk.
	files := test.WaitFiles(s.dataDir)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d\n", len(files))
	}

	// Because we haven't sent a tick yet, the 2nd sender goroutine which
	// sends the data (HTTP POST) should not have sent anything yet.
	posted := test.WaitPost(s.postChan)
	if posted != nil {
		t.Fatalf("Sender sent, expected no data: %s\n", string(posted))
	}

	// Now send a tick to simulate that it's time to send data.
	// The sender should sent (POST) the original data.
	s.tickerChan <- true
	posted = test.WaitPost(s.postChan)
	t.Assert(string(posted), Equals, `{"Ts":"2013-12-12T15:00:00Z","Level":1,"Service":"mm","Msg":"hello world"}`)

	// After successfully sending the data, the sender should remove the file.
	files = test.WaitFiles(s.dataDir)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}

	/**
	 * Try to stop it by closing the ticker chan.  This is important because it allows
	 * the agent to change data dirs by stopping one send and starting another with the
	 * same dataChan.
	 */
	close(s.tickerChan)
	s.tickerChan = nil

	files = test.WaitFiles(s.dataDir)
	if len(files) > 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}

	posted = test.WaitPost(s.postChan)
	if posted != nil {
		t.Fatalf("Expected no data, got %s\n", string(posted))
	}

	// Data send should log its shutdown (or crash).
	logs := test.WaitLogChan(s.logChan, 2)
	if len(logs) < 2 {
		t.Fatalf("Expected 2 log entries, got %+v", logs)
	}
	if logs[0].Msg != "sendData stop" || logs[1].Msg != "spoolData stop" {
		t.Error("Wrong log messages: %+v", logs)
	}

	// Pretend tool sends data while data sender is down/being changed.
	// This data should not be lost; it should be read by the new data sender.
	logEntry.Service = "qan"
	s.dataChan <- logEntry

	// Start a new sender: same dataChan but different data dir.
	newDir, _ := ioutil.TempDir("/tmp", "pct-data-sender2")
	defer func() { os.RemoveAll(newDir) }()
	s.tickerChan = make(chan bool, 1) // recreate because we closed it ^
	dataSender = data.NewSender(s.logger, s.client, s.dataChan, newDir, s.tickerChan)
	t.Assert(dataSender, NotNil)
	dataSender.Start()

	// New data should only be in the new dir.
	files = test.WaitFiles(newDir)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file in new dir, got %d\n", len(files))
	}
	files = test.WaitFiles(s.dataDir)
	if len(files) != 0 {
		t.Fatalf("Expected no files in old dir, got %d\n", len(files))
	}

	s.tickerChan <- true
	posted = test.WaitPost(s.postChan)
	t.Assert(string(posted), Equals, `{"Ts":"2013-12-12T15:00:00Z","Level":1,"Service":"qan","Msg":"hello world"}`)

	files = test.WaitFiles(newDir)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}
}
