package agent_test

import (
	"os"
	"os/user"
	"time"
	//"log"
	"fmt"
	"encoding/json"
	. "launchpad.net/gocheck"
	"testing"
	// our pkgs
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/service"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	// test and mock
	"github.com/percona/percona-cloud-tools/test"
	"github.com/percona/percona-cloud-tools/test/mock"
	"github.com/percona/percona-cloud-tools/test/mock/ws-client"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type AgentTestSuite struct {
	agentConfigFile string
	tmpLogFile string
	// agent and what it needs
	agent *agent.Agent
	config *agent.Config
	logRelayer *agentLog.LogRelayer
	cc *agent.ControlChannels
	cmdClient *ws_client.MockClient
	statusClient *ws_client.MockClient
	services map[string]service.Manager
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

	s.config = &agent.Config{
		ApiUrl: "wss://cloud-api.percona.com",
		ApiKey: "123abc",
		AgentUuid: "456-def-789",
		DataDir: "/var/spool/pct-agentd",
		LogFile: "/var/log/pct-agentd.log",
		LogLevel: "warn",
		PidFile: "/var/run/pct-agentd.pid",
		ConfigFile: "/etc/percona/pct-agentd.conf",
	}

	s.agentConfigFile = fmt.Sprintf("/tmp/pct-agentd.conf.%d", os.Getpid())

	logChan := make(chan *agentLog.LogEntry, 10)
	s.tmpLogFile = fmt.Sprintf("/tmp/pct-agentd.%d", os.Getpid())
	logger, _ := agentLog.OpenLogFile(s.tmpLogFile)
	s.logRelayer = agentLog.NewLogRelayer(new(mock.NullClient), logChan, logger, agentLog.LOG_LEVEL_DEBUG)

	s.cc = &agent.ControlChannels{
		LogChan: logChan,
		StopChan: make(chan bool),
		DoneChan: make(chan bool, 1),
	};

	s.cmdClient = ws_client.NewMockClient(s.dataToCmdClient, s.dataFromCmdClient, s.msgToCmdClient, s.msgFromCmdClient)
	s.statusClient = ws_client.NewMockClient(s.dataToStatusClient, s.dataFromStatusClient, s.msgToStatusClient, s.msgFromStatusClient)

	services := make(map[string]service.Manager)
	traceChan := make(chan string, 10)
	mockService := mock.NewMockServiceManager(traceChan)
	services["qh"] = mockService
	s.services = services
}

func (s *AgentTestSuite) TearDownSuite(t *C) {
	os.Remove(s.agentConfigFile)
	os.Remove(s.tmpLogFile)
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

	// Prevent agent from using /etc/percona/pct-agentd.conf.
	s.agent.ConfigFile = s.agentConfigFile

	// Start the agent.  It is receiving on our msgToCmdClient and msgToStatusClient.
	go s.agent.Run()
}

/////////////////////////////////////////////////////////////////////////////
// Test cases
// //////////////////////////////////////////////////////////////////////////

func (s *AgentTestSuite) TestHello(t *C) {
	got := s.agent.Hello()
	h, _ := os.Hostname()
	u, _ := user.Current()
	expect := map[string]string{
		// agent.config
		"ApiUrl": "wss://cloud-api.percona.com",
		"ApiKey": "123abc",
		"AgentUuid": "456-def-789",
		"DataDir": "/var/spool/pct-agentd",
		"LogFile": "/var/log/pct-agentd.log",
		"LogLevel": "warn",
		"PidFile": "/var/run/pct-agentd.pid",
		"ConfigFile": "/etc/percona/pct-agentd.conf",
		"DbDsn": "",
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
		Cmd: "status",
		Timeout: 3,
	}
	s.msgToStatusClient <-statusCmd

	// Tell the agent to stop then wait for it.
	test.DoneWait(s.cc)

	// Get msgs sent by agent to API (i.e. us).  There should only
	// be one: a proto.StatusReply.
	got := test.WaitForClientMsgs(s.msgFromStatusClient)
	t.Check(len(got), Equals, 1)

	// The agent should have sent back the original cmd's routing info
	// (user and id) with Data=StatusReply.
	expect := statusCmd
	statusReply := &proto.StatusReply{
		Agent: "- Wait listen",
		CmdQueue: make([]string, agent.CMD_QUEUE_SIZE),
		Service: map[string]string{
			"qh": "AOK",
		},
	}
	expect.Data, _ = json.Marshal(statusReply)
	t.Check(got[0].User, Equals, expect.User) // same user
	t.Check(got[0].Id, Equals, expect.Id) // same id
	t.Check(string(got[0].Data), Equals, string(expect.Data)) // status reply
}

func (s *AgentTestSuite) TestCmdChan(t *C) {
	newConfigFile := fmt.Sprintf("/tmp/pct-agentd.conf.%d.NEW", os.Getpid());
	defer func() {
		os.Remove(newConfigFile)
	}()

	newConfig := &agent.Config{
		ApiUrl: "wss://cloud-api.percona.com",
		ApiKey: "123abc",
		AgentUuid: "456-def-789",
		DataDir: "/var/spool/pct-agentd",
		LogFile: "/var/log/pct-agentd.log",
		LogLevel: "error",
		PidFile: "/var/run/pct-agentd.pid",
		ConfigFile: newConfigFile,
	}

	// This is what the API would send:
	data, _ := json.Marshal(newConfig)
	setConfigCmd := &proto.Msg{
		Ts: time.Now(),
		User: "daniel",
		Id: 1,
		Cmd: "SetConfig",
		Timeout: 3,
		Data: data,
	}
	s.msgToCmdClient <-setConfigCmd

	// Tell the agent to stop then wait for it.
	test.DoneWait(s.cc)

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

	// @todo check newConfigFile was written with new values
}
