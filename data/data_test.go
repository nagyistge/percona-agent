package data_test

import (
	"os"
	"fmt"
	"time"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"testing"
	pct "github.com/percona/cloud-tools"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-protocol/test"
	"github.com/percona/cloud-protocol/test/mock"
	// Testing
	"github.com/percona/cloud-tools/data"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct{
	logChan chan *proto.LogEntry
	logger *pct.Logger
	postChan chan []byte
	client proto.HttpClient
	dataChan chan interface{}
	dataDir string
	tickerChan chan bool
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "data_test")

	s.postChan = make(chan []byte, 2)
	s.client = &mock.HttpClient{PostChan: s.postChan}

	s.dataChan = make(chan interface{}, 1)

	dir, _ := ioutil.TempDir("", "data_test")
	s.dataDir = dir

	s.tickerChan = make(chan bool, 1)
}

func (s *TestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.dataDir); err != nil {
		fmt.Println(err)
	}
}

func (s *TestSuite) TestDataSender(t *C) {
	// Create the data sender
	dataSender := data.NewSender(s.logger, s.client, s.dataChan, s.dataDir, s.tickerChan)
	t.Assert(dataSender, NotNil)

	// Run the data sender: one gorutine receives data on dataChan and writes
	// it to disk, so let's test that first...
	dataSender.Start()

	// Doesn't matter what data we send; just send some bytes...
	ts, _ := time.Parse("2006-01-02 15:04:05", "2013-12-12 15:00:00")
	data := &proto.LogEntry{
		Ts: ts,
		Level: 1,
		Service: "mm",
		Msg: "hello world",
	}
	s.dataChan <-data

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
	s.tickerChan <-true
	time.Sleep(10 * time.Millisecond) // yield thread
	posted = test.WaitPost(s.postChan)
	t.Assert(string(posted), Equals, `{"Ts":"2013-12-12T15:00:00Z","Level":1,"Service":"mm","Msg":"hello world"}`)

	// After successfully sending the data, the sender should remove the file.
	files = test.WaitFiles(s.dataDir)
	if len(files) != 0 {
		t.Fatalf("Expected no files, got %d\n", len(files))
	}
}
