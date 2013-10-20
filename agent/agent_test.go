package agent_test

import (
	"os"
	"os/user"
	"time"
	//"log"
	"fmt"
	"io/ioutil"
	"encoding/json"
	. "launchpad.net/gocheck"
	"testing"
	// our pkgs
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/service"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/qa"
	// test and mock
	"github.com/percona/percona-cloud-tools/test"
	"github.com/percona/percona-cloud-tools/test/mock"
	"github.com/percona/percona-cloud-tools/test/mock/ws-client"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type AgentTestSuite struct {
	tmpDir string
	// agent and what it needs
	agent *agent.Agent
	config *agent.Config
	logRelayer *agentLog.LogRelayer
	logWriter *agentLog.LogWriter
	logEntriesChan chan interface{}
	cc *agent.ControlChannels
	cmdClient *ws_client.MockClient
	statusClient *ws_client.MockClient
	services map[string]service.Manager
	readyChan chan bool
	traceChan chan string
	// mock ws client chans
	dataToCmdClient chan interface{}
	dataFromCmdClient chan interface{}
	msgToCmdClient chan *proto.Msg
	msgFromCmdClient chan *proto.Msg
	// --
	dataToStatusClient chan interface{}
	dataFromStatusClient chan interface{}
	msgToStatusClient chan *proto.Msg
	msgFromStatusClient chan *proto.Msg
}
var _ = Suite(&AgentTestSuite{})

func (s *AgentTestSuite) SetUpSuite(t *C) {
	s.dataToCmdClient = make(chan interface{}, 10)
	s.dataFromCmdClient = make(chan interface{}, 10)
	s.msgToCmdClient = make(chan *proto.Msg, 10)
	s.msgFromCmdClient = make(chan *proto.Msg, 10)

	s.dataToStatusClient = make(chan interface{}, 10)
	s.dataFromStatusClient = make(chan interface{}, 10)
	s.msgToStatusClient = make(chan *proto.Msg, 10)
	s.msgFromStatusClient = make(chan *proto.Msg, 10)

	// Create fake agent config for testing.  Defaults for these won't work
	// because we're probably not root, so we can't write to /var/log/, etc.
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "pt-agentd")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := s.tmpDir + "/data"
	pidFile := s.tmpDir + "/pid"
	configFile := s.tmpDir + "/conf"
	logFile := s.tmpDir + "/log"
	s.config = &agent.Config{
		File: configFile,
		ApiUrl: "wss://cloud-api.percona.com",
		ApiKey: "123abc",
		AgentUuid: "456-def-789",
		DataDir: dataDir,
		PidFile: pidFile,
		LogFile: logFile,
		LogLevel: "debug",
	}

	logChan := make(chan *agentLog.LogEntry, 10)
	s.logEntriesChan = make(chan interface{}, 10)
	logger, _ := agentLog.OpenLogFile(logFile)
	s.logWriter = agentLog.NewLogWriter(logChan, "agent-test")
	s.logRelayer = agentLog.NewLogRelayer(mock.NewMockLogClient(s.logEntriesChan), logChan, logger, agentLog.LOG_LEVEL_DEBUG)
	go s.logRelayer.Run()

	s.cc = &agent.ControlChannels{
		LogChan: logChan,
		StopChan: make(chan bool, 1),
		DoneChan: make(chan bool),
	};

	s.cmdClient = ws_client.NewMockClient(s.dataToCmdClient, s.dataFromCmdClient, s.msgToCmdClient, s.msgFromCmdClient)
	s.statusClient = ws_client.NewMockClient(s.dataToStatusClient, s.dataFromStatusClient, s.msgToStatusClient, s.msgFromStatusClient)

	services := make(map[string]service.Manager)
	s.readyChan = make(chan bool, 1)
	s.traceChan = make(chan string, 10)
	services["qa"] = mock.NewMockServiceManager("qa", s.readyChan, s.traceChan)
	services["mm"] = mock.NewMockServiceManager("mm", s.readyChan, s.traceChan)
	s.services = services
}

func (s *AgentTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		fmt.Println(err)
	}
}

func (s *AgentTestSuite) SetUpTest(t *C) {
	// Before each test, create and agent.  Tests make change the agent,
	// so this ensures each test starts with an agent with known values.
	s.agent = agent.NewAgent(
		s.config,
		s.logRelayer,
		s.cc,
		s.cmdClient,
		s.statusClient,
		s.services,
	)

	// Start the agent.  It is receiving on our msgToCmdClient and msgToStatusClient.
	go s.agent.Run()
}

func (s *AgentTestSuite) TearDownTest(t *C) {
	if test.DoneWait(s.cc) == false {
		t.Fatal("Agent did not stop")
	}

	// Drain the channels before the next test.
	_ = test.WaitForLogEntries(s.logEntriesChan)
	_ = test.WaitForClientMsgs(s.msgFromCmdClient)
	_ = test.WaitForClientMsgs(s.msgFromStatusClient)
	_ = test.WaitForTraces(s.traceChan)
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *AgentTestSuite) TestHello(t *C) {
	// Agent hello data should include its config plus the hostname and username.
	got := s.agent.Hello()
	h, _ := os.Hostname()
	u, _ := user.Current()
	expect := map[string]string{
		// agent.config
		"File": s.config.File,
		"ApiUrl": "wss://cloud-api.percona.com",
		"ApiKey": "123abc",
		"AgentUuid": "456-def-789",
		"DataDir": s.config.DataDir,
		"LogFile": s.config.LogFile,
		"LogLevel": "debug",
		"PidFile": s.config.PidFile,
		// extra info
		"Hostname": h,
		"Username": u.Username,
	}
	t.Check(got, DeepEquals, expect)
}

func (s *AgentTestSuite) TestStatus(t *C) {
	// This is what the API would send:
	statusCmd := &proto.Msg{
		Ts: time.Now(),
		User: "daniel",
		Id: 1,
		Cmd: "Status",
		Timeout: 3,
	}
	s.msgToStatusClient <-statusCmd

	// Tell the agent to stop then wait for it.
	// test.DoneWait(s.cc)

	// Get msgs sent by agent to API (i.e. us).  There should only
	// be one: a proto.StatusReply.
	got := test.WaitForClientMsgs(s.msgFromStatusClient)
	t.Check(len(got), Equals, 1)

	// The agent should have sent back the original cmd's routing info
	// (user and id) with Data=StatusReply.
	expect := statusCmd
	statusReply := &proto.StatusReply{
		Agent: "Agent: Ready\nCommand: Ready\nStatus: 0\n",
		CmdQueue: make([]string, agent.CMD_QUEUE_SIZE),
		Service: map[string]string{
			"qa": "AOK",
			"mm": "AOK",
		},
	}
	expect.Data, _ = json.Marshal(statusReply)
	t.Check(got[0].User, Equals, expect.User) // same user
	t.Check(got[0].Id, Equals, expect.Id) // same id
	t.Check(string(got[0].Data), Equals, string(expect.Data)) // status reply
}

func (s *AgentTestSuite) TestSetConfig(t *C) {
	// s.config has the config we created in SetUpSuite.  Now tell the agent
	// to set (i.e. use) this config.  To really test that the agent can set
	// and change its config, we use new values for these:
	newConfig := &agent.Config{
		File: s.config.File,
		ApiUrl: s.config.ApiUrl,
		ApiKey: s.config.ApiKey,
		AgentUuid: s.config.AgentUuid,
		DataDir: s.config.DataDir,
		PidFile: s.config.PidFile + "-new",
		LogFile: s.config.LogFile + "-new",
		LogLevel: "warn",
	}

	// This is what the API would send:
	configData, _ := json.Marshal(newConfig)
	setConfigCmd := &proto.Msg{
		Ts: time.Now(),
		User: "daniel",
		Id: 1,
		Cmd: "SetConfig",
		Timeout: 3,
		Data: configData,
	}
	s.msgToCmdClient <-setConfigCmd

	// Tell the agent to stop then wait for it.
	// test.DoneWait(s.cc)

	// Get msgs sent by agent to API (i.e. us).  There should only
	// be one: a proto.CmdReply.
	got := test.WaitForClientMsgs(s.msgFromCmdClient)
	if t.Check(len(got), Equals, 1) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.FailNow()
	}

	// The agent should not have sent anything via the status client.
	gotStatus := test.WaitForClientMsgs(s.msgFromStatusClient)
	t.Check(len(gotStatus), Equals, 0)

	// The agent should have sent back the original cmd's routing info
	// (user and id) with Data=CmdReply.
	expect := setConfigCmd
	cmdReply := &proto.CmdReply{
		Error: nil,
	}
	expect.Data, _ = json.Marshal(cmdReply)
	t.Check(got[0].User, Equals, expect.User) // same user
	t.Check(got[0].Id, Equals, expect.Id) // same id
	t.Check(string(got[0].Data), Equals, string(expect.Data)) // status reply

	// The agent should write the config to Config.File.
	gotData, _ := ioutil.ReadFile(s.config.File)
	t.Check(string(gotData), Equals, string(configData))

	// The agent should write its PID to Config.PidFile.
	gotData, _ = ioutil.ReadFile(newConfig.PidFile)
	t.Check(string(gotData), Equals, fmt.Sprintf("%d\n", os.Getpid()))

	// The agent should open the new Config.LogFile.
	gotLogFile, _ := os.Stat(newConfig.LogFile)
	t.Check(gotLogFile, NotNil)
	if gotLogFile != nil {
		// FileInfo.Name() returns the base name
		t.Check(gotLogFile.Name(), Equals, "log-new")
	}
	// And check that it set the new log level to "warn":
	s.logWriter.Info("some info") // no logged
	s.logWriter.Warn("a warning")
	s.logWriter.Error("an error")
	gotLogEntries := test.WaitForLogEntries(s.logEntriesChan)
	expectLogEntries := []agentLog.LogEntry{
		{"", 0, 2, "pct-agentd", "Running agent"}, // before level was changed
		{"", 0, 3, "agent-test", "a warning"},
		{"", 0, 4, "agent-test", "an error"},
	}
	t.Check(gotLogEntries, DeepEquals, expectLogEntries)
}

func (s *AgentTestSuite) TestStartService(t *C) {
	// This is what the API would send:
	// First, the service's config:
	qaConfig := &qa.Config{
		Interval: 60, // seconds
		LongQueryTime: 0.123,
		MaxSlowLogSize: 1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries: true,
		MysqlDsn: "",
		MaxWorkers: 2,
		WorkerRuntime: 120, // seconds
		DataDir: "/var/lib/percona/qa",
	}
	qaConfigData, _ := json.Marshal(qaConfig)
	// Second, a ServiceMsg:
	serviceMsg := &proto.ServiceMsg{
		Name: "qa",
		Config: qaConfigData,
	}
	serviceMsgData, _ := json.Marshal(serviceMsg)
	// Finally, the Msg:
	msg := &proto.Msg{
		Ts: time.Now(),
		User: "daniel",
		Id: 1,
		Cmd: "StartService",
		Timeout: 3,
		Data: serviceMsgData,
	}

	// The readyChan is used by mock.MockServiceManager.Start() and Stop()
	// to simulate slow starts and stops.  We're not testing that here, so
	// this lets the service start immediately.
	s.readyChan <-true

	// Send the msg to the client, tell the agent to stop, then wait for it.
	s.msgToCmdClient <-msg

	// The agent should first check that the service service is *not* running,
	// then start it.  It should do this only for the requested service (qa).
	got := test.WaitForTraces(s.traceChan)
	expect := []string{
		`IsRunning qa`,
		`Start qa {"Interval":60,"LongQueryTime":0.123,"MaxSlowLogSize":1073741824,"RemoveOldSlowLogs":true,"ExampleQueries":true,"MysqlDsn":"","MaxWorkers":2,"WorkerRuntime":120,"DataDir":"/var/lib/percona/qa"}`,
	}
	t.Check(got, DeepEquals, expect)

	// The reply to that ^ should be Error=nil.
	gotReplies := test.WaitForClientMsgs(s.msgFromCmdClient)
	if t.Check(len(gotReplies), Equals, 1) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}
	reply := new(proto.CmdReply)
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, IsNil)
	
}

// See TestStartService ^.  This test is like it, but it simulates a slow start.
func (s *AgentTestSuite) TestStartServiceSlow(t *C) {
	qaConfig := &qa.Config{
		Interval: 60, // seconds
		LongQueryTime: 0.123,
		MaxSlowLogSize: 1073741824, // 1 GiB
		RemoveOldSlowLogs: true,
		ExampleQueries: true,
		MysqlDsn: "",
		MaxWorkers: 2,
		WorkerRuntime: 120, // seconds
		DataDir: "/var/lib/percona/qa",
	}
	qaConfigData, _ := json.Marshal(qaConfig)
	serviceMsg := &proto.ServiceMsg{
		Name: "qa",
		Config: qaConfigData,
	}
	serviceMsgData, _ := json.Marshal(serviceMsg)
	now := time.Now()
	msg := &proto.Msg{
		Ts: now,
		User: "daniel",
		Id: 1,
		Cmd: "StartService",
		Timeout: 3,
		Data: serviceMsgData,
	}

	// Send the msg to the client, tell the agent to stop, then wait for it.
	s.msgToCmdClient <-msg

	// No replies yet.
	gotReplies := test.WaitForClientMsgs(s.msgFromCmdClient)
	if t.Check(len(gotReplies), Equals, 0) == false {
		// Avoid "index out of range" panic by trying to access got[0] below.
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}

	// Agent should be able to reply on status chan, indicating that it's
	// still starting the service.
	gotStatus := test.GetStatus(s.msgToStatusClient, s.msgFromStatusClient)
	t.Check(gotStatus.Agent, Equals,
		fmt.Sprintf("Agent: Ready\nCommand: StartService [%s]\nStatus: 0\n", msg));

	// Make it seem like service has started now.
	time.Sleep(1 * time.Second)
	s.readyChan <-true
	// test.DoneWait(s.cc)

	// Agent sends reply: no error.
	gotReplies = test.WaitForClientMsgs(s.msgFromCmdClient)
	if t.Check(len(gotReplies), Equals, 1) == false {
		t.Errorf("%q", gotReplies)
		t.FailNow()
	}
	reply := new(proto.CmdReply)
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, IsNil)
}
