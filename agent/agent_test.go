package agent_test

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/logrelay"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
	"io/ioutil"
	"launchpad.net/gocheck"
	"os"
	"testing"
	"time"
)

// Hook gocheck into the "go test" runner.
func Test(t *testing.T) { gocheck.TestingT(t) }

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/agent"

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

var _ = gocheck.Suite(&AgentTestSuite{})

func (s *AgentTestSuite) SetUpSuite(t *gocheck.C) {
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

	s.sendChan = make(chan *proto.Cmd, 5)
	s.recvChan = make(chan *proto.Reply, 5)
	s.sendDataChan = make(chan interface{}, 5)
	s.recvDataChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(s.sendChan, s.recvChan, s.sendDataChan, s.recvDataChan)
	go s.client.Run()

	s.readyChan = make(chan bool, 2)
	s.traceChan = make(chan string, 10)

	s.doneChan = make(chan bool, 1)
}

func (s *AgentTestSuite) TearDownSuite(t *gocheck.C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		fmt.Println(err)
	}
}

func (s *AgentTestSuite) SetUpTest(t *gocheck.C) {
	// Before each test, create an agent.  Tests make change the agent,
	// so this ensures each test starts with an agent with known values.
	services := make(map[string]pct.ServiceManager)
	services["qan"] = mock.NewMockServiceManager("Qan", s.readyChan, s.traceChan)
	services["mm"] = mock.NewMockServiceManager("Mm", s.readyChan, s.traceChan)
	s.agent = agent.NewAgent(s.config, s.auth, s.logRelay, s.logger, s.client, services)

	// Run the agent.
	go func() {
		s.stopReason, s.upgrade = s.agent.Run()
		s.doneChan <- true
	}()
}

func (s *AgentTestSuite) TearDownTest(t *gocheck.C) {
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

func (s *AgentTestSuite) TestStatus(t *gocheck.C) {

	// This is what the API would send:
	statusCmd := &proto.Cmd{
		Ts:   time.Now(),
		User: "daniel@percona.com",
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
		Qan:             "",
		Mm:              "",
	}
	if ok, diff := test.IsDeeply(gotReply, expectReply); !ok {
		t.Error(diff)
	}
}

func (s *AgentTestSuite) TestStartStopService(t *gocheck.C) {
	// To start a service, first we make a config for the service:
	qanConfig := &qan.Config{
		Interval:          60,         // seconds
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries:    true,
		MaxWorkers:        2,
		WorkerRunTime:     120, // seconds
	}

	// Second, the service config is encoded and encapsulated in a ServiceData:
	qanConfigData, _ := json.Marshal(qanConfig)
	serviceCmd := &proto.ServiceData{
		Name:   "qan",
		Config: qanConfigData,
	}

	// Third and final, the service data is encoded and encapsulated in a Cmd:
	serviceData, _ := json.Marshal(serviceCmd)
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

	// Send the StartService cmd to the client, then wait for the reply
	// which should not have an error, indicating success.
	s.sendChan <- cmd
	gotReplies := test.WaitReply(s.recvChan)
	if len(gotReplies) != 1 {
		t.Fatal("Got Reply to Cmd:StartService")
	}
	reply := &proto.Reply{}
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	if reply.Error != "" {
		t.Error("No Reply.Error to Cmd:StartService; got ", reply.Error)
	}

	// To double-check that the agent started without error, get its status
	// which should show everything is "Ready".
	status := test.GetStatus(s.sendChan, s.recvChan)
	expectStatus := &proto.StatusData{
		Agent:           "Ready",
		AgentCmdHandler: "Ready",
		AgentCmdQueue:   []string{},
		Qan:             "Ready", // fake
		QanLogParser:    "",
		Mm:              "", // fake
		MmMonitors:      make(map[string]string),
	}
	if same, diff := test.IsDeeply(status, expectStatus); !same {
		t.Error(diff)
	}

	// Finally, since we're using mock objects, let's double check the
	// execution trace, i.e. what calls the agent made based on all
	// the previous ^.
	got := test.WaitTrace(s.traceChan)
	expect := []string{
		`IsRunning Qan`,
		`Start Qan ` + string(qanConfigData),
		`Status Qan`,
		`Status Mm`,
	}
	t.Check(got, gocheck.DeepEquals, expect)

	/**
	 * Stop the service.
	 */

	serviceCmd = &proto.ServiceData{
		Name: "qan",
	}
	serviceData, _ = json.Marshal(serviceCmd)
	cmd = &proto.Cmd{
		Ts:   time.Now(),
		User: "daniel",
		Cmd:  "StopService",
		Data: serviceData,
	}

	// Let fake qan service stop immediately.
	s.readyChan <- true

	s.sendChan <- cmd
	gotReplies = test.WaitReply(s.recvChan)
	if len(gotReplies) != 1 {
		t.Fatal("Got Reply to Cmd:StopService")
	}
	reply = &proto.Reply{}
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	if reply.Error != "" {
		t.Error("No Reply.Error to Cmd:StopService; got ", reply.Error)
	}

	status = test.GetStatus(s.sendChan, s.recvChan)
	expectStatus = &proto.StatusData{
		Agent:           "Ready",
		AgentCmdHandler: "Ready",
		AgentCmdQueue:   []string{},
		Qan:             "Stopped", // fake
		QanLogParser:    "",
		Mm:              "",
		MmMonitors:      make(map[string]string),
	}
	if same, diff := test.IsDeeply(status, expectStatus); !same {
		t.Error(diff)
	}
}

func (s *AgentTestSuite) TestStartServiceSlow(t *gocheck.C) {
	// This test is like TestStartService but simulates a slow starting service.

	qanConfig := &qan.Config{
		Interval:          60,         // seconds
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
	if t.Check(len(gotReplies), gocheck.Equals, 0) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}

	// Agent should be able to reply on status chan, indicating that it's
	// still starting the service.
	gotStatus := test.GetStatus(s.sendChan, s.recvChan)
	t.Check(gotStatus.Agent, gocheck.Equals, "Ready")
	t.Check(gotStatus.AgentCmdQueue, gocheck.DeepEquals, []string{cmd.String()})

	// Make it seem like service has started now.
	// time.Sleep(1 * time.Second)
	s.readyChan <- true

	// Agent sends reply: no error.
	gotReplies = test.WaitReply(s.recvChan)
	if t.Check(len(gotReplies), gocheck.Equals, 1) == false {
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}
	reply := new(proto.Reply)
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, gocheck.Equals, "")
}

func (s *AgentTestSuite) TestLoadConfig(t *gocheck.C) {
	// Load a partial config to make sure LoadConfig() works in general but also
	// when the config has missing options (which is normal).
	config := agent.LoadConfig(sample + "/config001.json")
	expect := &agent.Config{
		ApiKey:    "123",
		AgentUuid: "abc-123-def",
		LogLevel:  "error",
	}
	if same, diff := test.IsDeeply(config, expect); !same {
		// @todo: if expect is not ptr, IsDeeply dies with "got ptr, expected struct"
		t.Error(diff)
		t.Logf("got: %+v", config)
	}

	// Load a config with all options to make sure LoadConfig() hasn't missed any.
	fullConfig := agent.LoadConfig(sample + "/full_config.json")
	expect = &agent.Config{
		ApiHostname: "agent hostname",
		ApiKey:      "api key",
		AgentUuid:   "agent uuid",
		PidFile:     "pid file",
		LogFile:     "log file",
		LogLevel:    "info",
		DataDir:     "data dir",
		Links:       map[string]string{"home": "/"},
		Enable:      []string{"enabled"},
		Disable:     []string{"disabled"},
	}
	if same, diff := test.IsDeeply(fullConfig, expect); !same {
		t.Error(diff)
		t.Logf("got: %+v", config)
	}
}

func (s *AgentTestSuite) TestApplyConfig(t *gocheck.C) {
	// When we apply config2 to config1, certain values (that are set)
	// in config2 should apply/overwrite the values in config1.
	config1 := agent.LoadConfig(sample + "/config001.json")
	config2 := agent.LoadConfig(sample + "/config002.json")
	config1.Apply(config2)
	expect := &agent.Config{
		ApiKey:    "123",                    // config1
		AgentUuid: "abc-123-def",            // config1
		LogLevel:  "warning",                // config2
		LogFile:   "/tmp/percona-agent.log", // new
		Disable:   []string{"LogFile"},      // new
	}
	// @todo: if expect is not ptr, IsDeeply dies with "got ptr, expected struct"
	if same, diff := test.IsDeeply(config1, expect); !same {
		t.Error(diff)
		t.Logf("got: %+v", config1)
	}
}

func (s *AgentTestSuite) TestEnableDisableConfig(t *gocheck.C) {
	config := agent.LoadConfig(sample + "/config003.json")
	if !config.Enabled("Turbo") {
		t.Error("Turbo enabled in config003.json")
		t.Logf("got: %+v", config)
	}
	if config.Enabled("Foo") {
		t.Error("Foo is not enabled in config003.json")
		t.Logf("got: %+v", config)
	}
	if !config.Disabled("Crashing") {
		t.Error("Crashing is disabled in config003.json")
		t.Logf("got: %+v", config)
	}
	if config.Disabled("Bar") {
		t.Error("Bar is not disabled in config003.json")
		t.Logf("got: %+v", config)
	}
}

func (s *AgentTestSuite) TestRequiredConfig(t *gocheck.C) {
	// Apply an empty config to a full config.  The "required" options in
	// the full config (ApiKey, LogFile, etc.) should *not* be overwritten
	// with empty strings from the empty config.
	emptyConfig := agent.LoadConfig(sample + "/empty_config.json")
	fullConfig := agent.LoadConfig(sample + "/full_config.json")
	fullConfig.Apply(emptyConfig)
	expect := &agent.Config{
		ApiHostname: "agent hostname",
		ApiKey:      "api key",
		AgentUuid:   "agent uuid",
		PidFile:     "",
		LogFile:     "log file",
		LogLevel:    "info",
		DataDir:     "data dir",
		Links:       map[string]string{},
		Enable:      nil,
		Disable:     nil,
	}
	if same, diff := test.IsDeeply(fullConfig, expect); !same {
		t.Error(diff)
		t.Logf("got: %+v", fullConfig)
	}

	// Reverse that ^: apply full config to empty config and we should get
	// the entire full config.
	fullConfig = agent.LoadConfig(sample + "/full_config.json")
	emptyConfig = agent.LoadConfig(sample + "/empty_config.json")
	emptyConfig.Apply(fullConfig)
	expect = &agent.Config{
		ApiHostname: "agent hostname",
		ApiKey:      "api key",
		AgentUuid:   "agent uuid",
		PidFile:     "pid file",
		LogFile:     "log file",
		LogLevel:    "info",
		DataDir:     "data dir",
		Links:       map[string]string{"home": "/"},
		Enable:      []string{"enabled"},
		Disable:     []string{"disabled"},
	}
	if same, diff := test.IsDeeply(fullConfig, expect); !same {
		t.Error(diff)
		t.Logf("got: %+v", fullConfig)
	}
}
