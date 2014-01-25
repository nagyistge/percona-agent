package agent_test

import (
	// Core
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"
	// External
	"github.com/percona/cloud-protocol/proto"
	// Internal
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/logrelay"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	// Testing
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	. "launchpad.net/gocheck"
	"testing"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type AgentTestSuite struct {
	tmpDir string
	// agent and what it needs
	config    *agent.Config
	auth      *proto.AgentAuth
	agent     *agent.Agent
	logRelay  *logrelay.LogRelay
	logger    *pct.Logger
	logChan   chan *proto.LogEntry
	client    pct.WebsocketClient
	services  map[string]pct.ServiceManager
	readyChan chan bool
	traceChan chan string
	// --
	sendDataChan chan interface{}
	recvDataChan chan interface{}
	sendChan     chan *proto.Cmd
	recvChan     chan *proto.Reply
	//
	doneChan   chan bool
	stopReason string
	upgrade    bool
}

var _ = Suite(&AgentTestSuite{})

func (s *AgentTestSuite) SetUpSuite(t *C) {
	// Tmp dir
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "pt-agentd")
	if err != nil {
		t.Fatal(err)
	}
	// dataDir := s.tmpDir + "/data"
	// pidFile := s.tmpDir + "/pid"
	// logFile := s.tmpDir + "/log"

	// Agent
	s.config = &agent.Config{
		ApiHostname: agent.API_HOSTNAME,
		LogFile:     agent.LOG_FILE,
		LogLevel:    agent.LOG_LEVEL,
		DataDir:     agent.DATA_DIR,
	}

	s.auth = &proto.AgentAuth{
		ApiKey:   "123",
		Uuid:     "abc-123",
		Hostname: "server1",
		Username: "root",
	}

	nullClient := mock.NewNullClient()
	s.logRelay = logrelay.NewLogRelay(nullClient, "", proto.LOG_INFO)
	go s.logRelay.Run()
	s.logChan = s.logRelay.LogChan()
	s.logger = pct.NewLogger(s.logChan, "agent-test")

	// mock client <-------------------------------------------------> API <---------------------> mock front end
	// handler <- agent <- chan <- client (agent on user's server)  <- API (cloud-api.percona.com) <- front end <- user
	//                      |         |                                  |                            |
	// handler <- agent <- chan <- mock client <-                    sendChan                [Cmd] <- test
	// handler -> agent -> chan -> mock client -> [Reply]            recvChan                      -> test
	s.sendChan = make(chan *proto.Cmd, 5)
	s.recvChan = make(chan *proto.Reply, 5)
	s.sendDataChan = make(chan interface{}, 5)
	s.recvDataChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(s.sendChan, s.recvChan, s.sendDataChan, s.recvDataChan)
	go s.client.Run()

	s.readyChan = make(chan bool, 2)
	s.traceChan = make(chan string, 10)
	services := make(map[string]pct.ServiceManager)
	services["qan"] = mock.NewMockServiceManager("Qan", s.readyChan, s.traceChan)
	services["mm"] = mock.NewMockServiceManager("Mm", s.readyChan, s.traceChan)
	s.services = services

	s.doneChan = make(chan bool, 1)
}

func (s *AgentTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		fmt.Println(err)
	}
}

func (s *AgentTestSuite) SetUpTest(t *C) {
	// Before each test, create and agent.  Tests make change the agent,
	// so this ensures each test starts with an agent with known values.
	s.agent = agent.NewAgent(s.config, s.auth, s.logRelay, s.logger, s.client, s.services)

	// Run the agent.
	go func() {
		s.stopReason, s.upgrade = s.agent.Run()
		s.doneChan <- true
	}()
}

func (s *AgentTestSuite) TearDownTest(t *C) {
	s.readyChan <- true                   // qan.Stop() immediately
	s.readyChan <- true                   // mm.Stop immediately
	s.sendChan <- &proto.Cmd{Cmd: "Stop"} // tell agent to stop itself
	<-s.doneChan                          // wait for goroutine agent.Run() in test
	test.DrainLogChan(s.logChan)
	test.DrainSendChan(s.sendChan)
	test.DrainRecvChan(s.recvChan)
	test.DrainTraceChan(s.traceChan)
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *AgentTestSuite) TestStatus(t *C) {

	// This is what the API would send:
	statusCmd := &proto.Cmd{
		Ts:   time.Now(),
		User: "daniel",
		Cmd:  "Status",
	}
	s.sendChan <- statusCmd

	// Get msgs sent by agent to API (i.e. us).  There should only
	// be one: a proto.StatusData.
	got := test.WaitReply(s.recvChan)
	if len(got) == 0 {
		t.Fatal("Got reply")
	}
	gotReply := proto.StatusData{}
	json.Unmarshal(got[0].Data, &gotReply)

	// The agent should have sent back the original cmd's routing info
	// (user and id) with Data=StatusData.
	expectReply := proto.StatusData{
		Agent:           "Ready",
		AgentCmdHandler: "Ready",
		AgentCmdQueue:   []string{},
		Qan:             "OK",
		Mm:              "OK",
	}
	if ok, diff := test.IsDeeply(gotReply, expectReply); !ok {
		t.Error(diff)
	}
}

func (s *AgentTestSuite) TestStartService(t *C) {
	// This is what the API would send:
	// First, the service's config:
	qanConfig := &qan.Config{
		Interval:          60, // seconds
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        2,
		WorkerRunTime:     120, // seconds
	}
	qanConfigData, _ := json.Marshal(qanConfig)
	qanConfigString := string(qanConfigData)
	// Second, a ServiceData:
	serviceCmd := &proto.ServiceData{
		Name:   "qan",
		Config: qanConfigData,
	}
	serviceData, _ := json.Marshal(serviceCmd)
	// Finally, the Cmd:
	cmd := &proto.Cmd{
		Ts:   time.Now(),
		User: "daniel",
		Cmd:  "StartService",
		Data: serviceData,
	}

	// The readyChan is used by mock.MockServiceManager.Start() and Stop()
	// to simulate slow starts and stops.  We're not testing that here, so
	// this lets the service start immediately.
	s.readyChan <- true

	// Send the cmd to the client, tell the agent to stop, then wait for it.
	s.sendChan <- cmd

	// The agent should first check that the service service is *not* running,
	// then start it.  It should do this only for the requested service (qan).
	got := test.WaitTrace(s.traceChan)
	expect := []string{
		`IsRunning Qan`,
		`Start Qan ` + qanConfigString,
	}
	t.Check(got, DeepEquals, expect)

	// The reply to that ^ should be Error=nil.
	gotReplies := test.WaitReply(s.recvChan)
	if t.Check(len(gotReplies), Equals, 1) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}
	reply := new(proto.Reply)
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, Equals, "")
}

// See TestStartService ^.  This test is like it, but it simulates a slow start.
func (s *AgentTestSuite) TestStartServiceSlow(t *C) {
	qanConfig := &qan.Config{
		Interval:          60, // seconds
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        2,
		WorkerRunTime:     120, // seconds
	}
	qanConfigData, _ := json.Marshal(qanConfig)
	serviceCmd := &proto.ServiceData{
		Name:   "qan",
		Config: qanConfigData,
	}
	serviceData, _ := json.Marshal(serviceCmd)
	now := time.Now()
	cmd := &proto.Cmd{
		Ts:   now,
		User: "daniel",
		Cmd:  "StartService",
		Data: serviceData,
	}

	// Send the cmd to the client, tell the agent to stop, then wait for it.
	s.sendChan <- cmd

	// No replies yet.
	gotReplies := test.WaitReply(s.recvChan)
	if t.Check(len(gotReplies), Equals, 0) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}

	// Agent should be able to reply on status chan, indicating that it's
	// still starting the service.
	gotStatus := test.GetStatus(s.sendChan, s.recvChan)
	t.Check(gotStatus.Agent, Equals, "Ready")
	t.Check(gotStatus.AgentCmdQueue, DeepEquals, []string{cmd.String()})

	// Make it seem like service has started now.
	// time.Sleep(1 * time.Second)
	s.readyChan <- true
	// test.DoneWait(s.cc)

	// Agent sends reply: no error.
	gotReplies = test.WaitReply(s.recvChan)
	if t.Check(len(gotReplies), Equals, 1) == false {
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}
	reply := new(proto.Reply)
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, Equals, "")
}
