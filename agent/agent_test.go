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

package agent_test

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
	"io/ioutil"
	golog "log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Hook gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = test.RootDir + "/agent"

type AgentTestSuite struct {
	tmpDir     string
	configFile string
	// Log
	logger  *pct.Logger
	logChan chan *proto.LogEntry
	// Agent
	agent        *agent.Agent
	config       *agent.Config
	services     map[string]pct.ServiceManager
	client       *mock.WebsocketClient
	sendDataChan chan interface{}
	recvDataChan chan interface{}
	sendChan     chan *proto.Cmd
	recvChan     chan *proto.Reply
	api          *mock.API
	// --
	readyChan      chan bool
	traceChan      chan string
	doneChan       chan bool
	stopReason     string
	upgrade        bool
	alreadyStopped bool
}

var _ = Suite(&AgentTestSuite{})

func (s *AgentTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configFile = filepath.Join(s.tmpDir, pct.CONFIG_DIR, "agent"+pct.CONFIG_FILE_SUFFIX)

	// Log
	// todo: use log.Manager instead
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "agent-test")

	// Agent
	s.config = &agent.Config{
		AgentUuid:   "abc-123-def",
		ApiKey:      "789",
		ApiHostname: agent.DEFAULT_API_HOSTNAME,
		Keepalive:   3600, // don't send while testing
	}

	s.sendChan = make(chan *proto.Cmd, 5)
	s.recvChan = make(chan *proto.Reply, 5)
	s.sendDataChan = make(chan interface{}, 5)
	s.recvDataChan = make(chan interface{}, 5)
	s.client = mock.NewWebsocketClient(s.sendChan, s.recvChan, s.sendDataChan, s.recvDataChan)
	s.client.ErrChan = make(chan error)

	s.readyChan = make(chan bool, 2)
	s.traceChan = make(chan string, 10)
	s.doneChan = make(chan bool, 1)
}

func (s *AgentTestSuite) SetUpTest(t *C) {
	// Before each test, create an agent.  Tests make change the agent,
	// so this ensures each test starts with an agent with known values.
	s.services = make(map[string]pct.ServiceManager)
	s.services["qan"] = mock.NewMockServiceManager("qan", s.readyChan, s.traceChan)
	s.services["mm"] = mock.NewMockServiceManager("mm", s.readyChan, s.traceChan)

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", s.config.ApiHostname, s.config.ApiKey, s.config.AgentUuid, links)

	s.agent = agent.NewAgent(s.config, s.logger, s.api, s.client, s.services)

	// Run the agent.
	go func() {
		s.agent.Run()
		s.doneChan <- true
	}()
}

func (s *AgentTestSuite) TearDownTest(t *C) {
	s.readyChan <- true // qan.Stop() immediately
	s.readyChan <- true // mm.Stop immediately

	if !s.alreadyStopped {
		s.sendChan <- &proto.Cmd{Cmd: "Stop"} // tell agent to stop itself
		select {
		case <-s.doneChan: // wait for goroutine agent.Run() in test
		case <-time.After(5 * time.Second):
			golog.Panic("Agent didn't respond to Stop cmd")
		}
	}

	test.DrainLogChan(s.logChan)
	test.DrainSendChan(s.sendChan)
	test.DrainRecvChan(s.recvChan)
	test.DrainTraceChan(s.traceChan)
	test.DrainTraceChan(s.client.TraceChan)
	test.DrainBoolChan(s.client.ConnectChan())
}

func (s *AgentTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
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

	got := test.WaitStatusReply(s.recvChan)
	t.Assert(got, NotNil)

	expectStatus := map[string]string{
		"agent": "Idle",
	}
	if ok, diff := test.IsDeeply(got, expectStatus); !ok {
		test.Dump(got)
		t.Error(diff)
	}

	// We asked for all status, so we should get mm too.
	_, ok := got["mm"]
	t.Check(ok, Equals, true)

	/**
	 * Get only agent's status
	 */
	statusCmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "Status",
		Service: "agent",
	}
	s.sendChan <- statusCmd
	got = test.WaitStatusReply(s.recvChan)
	t.Assert(got, NotNil)

	// Only asked for agent, so we shouldn't get mm.
	_, ok = got["mm"]
	t.Check(ok, Equals, false)

	/**
	 * Get only sub-service status.
	 */
	statusCmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "Status",
		Service: "mm",
	}
	s.sendChan <- statusCmd
	got = test.WaitStatusReply(s.recvChan)
	t.Assert(got, NotNil)

	// Asked for mm, so we get it.
	_, ok = got["mm"]
	t.Check(ok, Equals, true)

	// Didn't ask for all or agent, so we don't get it.
	_, ok = got["agent"]
	t.Check(ok, Equals, false)
}

func (s *AgentTestSuite) TestStatusAfterConnFail(t *C) {
	// Use optional ConnectChan in mock ws client for this test only.
	connectChan := make(chan bool)
	s.client.SetConnectChan(connectChan)
	defer s.client.SetConnectChan(nil)

	// Disconnect agent.
	s.client.Disconnect()

	// Wait for agent to reconnect.
	<-connectChan
	connectChan <- true

	// Send cmd.
	statusCmd := &proto.Cmd{
		Ts:   time.Now(),
		User: "daniel",
		Cmd:  "Status",
	}
	s.sendChan <- statusCmd

	// Get reply.
	got := test.WaitStatusReply(s.recvChan)
	t.Assert(got, NotNil)
	t.Check(got["agent"], Equals, "Idle")
}

func (s *AgentTestSuite) TestStartStopService(t *C) {
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
		Ts:      time.Now(),
		User:    "daniel",
		Service: "agent",
		Cmd:     "StartService",
		Data:    serviceData,
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
	// which should show everything is "Ready" or "Idle".
	status := test.GetStatus(s.sendChan, s.recvChan)
	expectStatus := map[string]string{
		"agent": "Idle",
		"qan":   "Ready",
		"mm":    "",
	}
	if same, diff := test.IsDeeply(status, expectStatus); !same {
		t.Error(diff)
	}

	// Finally, since we're using mock objects, let's double check the
	// execution trace, i.e. what calls the agent made based on all
	// the previous ^.
	got := test.WaitTrace(s.traceChan)
	expect := []string{
		`Start qan`,
		`Status qan`,
		`Status mm`,
	}
	t.Check(got, DeepEquals, expect)

	/**
	 * Stop the service.
	 */

	serviceCmd = &proto.ServiceData{
		Name: "qan",
	}
	serviceData, _ = json.Marshal(serviceCmd)
	cmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Service: "agent",
		Cmd:     "StopService",
		Data:    serviceData,
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
	t.Check(status["agent"], Equals, "Idle")
	t.Check(status["qan"], Equals, "Stopped")
	t.Check(status["mm"], Equals, "")
}

func (s *AgentTestSuite) TestStartServiceSlow(t *C) {
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
		Ts:      now,
		User:    "daniel",
		Service: "agent",
		Cmd:     "StartService",
		Data:    serviceData,
	}

	// Send the cmd to the client, tell the agent to stop, then wait for it.
	s.sendChan <- cmd

	// No replies yet.
	gotReplies := test.WaitReply(s.recvChan)
	if len(gotReplies) != 0 {
		t.Fatal("No reply before StartService")
	}

	// Agent should be able to reply on status chan, indicating that it's
	// still starting the service.
	gotStatus := test.GetStatus(s.sendChan, s.recvChan)
	if !t.Check(gotStatus["agent"], Equals, "Idle") {
		test.Dump(gotStatus)
	}

	// Make it seem like service has started now.
	s.readyChan <- true

	// Agent sends reply: no error.
	gotReplies = test.WaitReply(s.recvChan)
	if len(gotReplies) == 0 {
		t.Fatal("Get reply")
	}
	if len(gotReplies) > 1 {
		t.Errorf("One reply, got %+v", gotReplies)
	}

	reply := &proto.Reply{}
	_ = json.Unmarshal(gotReplies[0].Data, reply)
	t.Check(reply.Error, Equals, "")
}

func (s *AgentTestSuite) TestStartStopUnknownService(t *C) {
	// Starting an unknown service should return an error.
	serviceCmd := &proto.ServiceData{
		Name: "foo",
	}
	serviceData, _ := json.Marshal(serviceCmd)
	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Service: "agent",
		Cmd:     "StartService",
		Data:    serviceData,
	}

	s.sendChan <- cmd
	gotReplies := test.WaitReply(s.recvChan)
	t.Assert(len(gotReplies), Equals, 1)
	t.Check(gotReplies[0].Cmd, Equals, "StartService")
	t.Check(gotReplies[0].Error, Not(Equals), "")

	// Stopp an unknown service should return an error.
	cmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Service: "agent",
		Cmd:     "StopService",
		Data:    serviceData,
	}

	s.sendChan <- cmd
	gotReplies = test.WaitReply(s.recvChan)
	t.Assert(len(gotReplies), Equals, 1)
	t.Check(gotReplies[0].Cmd, Equals, "StopService")
	t.Check(gotReplies[0].Error, Not(Equals), "")
}

func (s *AgentTestSuite) TestLoadConfig(t *C) {
	// Load a partial config to make sure LoadConfig() works in general but also
	// when the config has missing options (which is normal).
	os.Remove(s.configFile)
	test.CopyFile(sample+"/config001.json", s.configFile)
	bytes, err := agent.LoadConfig()
	t.Assert(err, IsNil)
	got := &agent.Config{}
	if err := json.Unmarshal(bytes, got); err != nil {
		t.Fatal(err)
	}
	expect := &agent.Config{
		AgentUuid:   "abc-123-def",
		ApiHostname: agent.DEFAULT_API_HOSTNAME,
		ApiKey:      "123",
		Keepalive:   agent.DEFAULT_KEEPALIVE,
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		// @todo: if expect is not ptr, IsDeeply dies with "got ptr, expected struct"
		test.Dump(got)
		t.Error(diff)
	}

	// Load a config with all options to make sure LoadConfig() hasn't missed any.
	os.Remove(s.configFile)
	test.CopyFile(sample+"/full_config.json", s.configFile)
	bytes, err = agent.LoadConfig()
	t.Assert(err, IsNil)
	got = &agent.Config{}
	if err := json.Unmarshal(bytes, got); err != nil {
		t.Fatal(err)
	}
	expect = &agent.Config{
		ApiHostname: "agent hostname",
		ApiKey:      "api key",
		AgentUuid:   "agent uuid",
		Keepalive:   agent.DEFAULT_KEEPALIVE,
	}
	if same, diff := test.IsDeeply(got, expect); !same {
		test.Dump(got)
		t.Error(diff)
	}
}

func (s *AgentTestSuite) TestGetConfig(t *C) {
	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "GetConfig",
		Service: "agent",
	}
	s.sendChan <- cmd

	got := test.WaitReply(s.recvChan)
	t.Assert(len(got), Equals, 1)
	gotConfig := []proto.AgentConfig{}
	if err := json.Unmarshal(got[0].Data, &gotConfig); err != nil {
		t.Fatal(err)
	}

	config := *s.config
	config.Links = nil
	bytes, _ := json.Marshal(config)
	expect := []proto.AgentConfig{
		{
			InternalService: "agent",
			Config:          string(bytes),
			Running:         true,
		},
	}

	if ok, diff := test.IsDeeply(gotConfig, expect); !ok {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}
}

func (s *AgentTestSuite) TestGetVersion(t *C) {
	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "Version",
		Service: "agent",
	}
	s.sendChan <- cmd

	got := test.WaitReply(s.recvChan)
	t.Assert(len(got), Equals, 1)
	version := &proto.Version{}
	json.Unmarshal(got[0].Data, &version)
	t.Check(version.Running, Equals, agent.VERSION)
}

func (s *AgentTestSuite) TestSetConfigApiKey(t *C) {
	newConfig := *s.config
	newConfig.ApiKey = "101"
	data, err := json.Marshal(newConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "SetConfig",
		Service: "agent",
		Data:    data,
	}
	s.sendChan <- cmd

	got := test.WaitReply(s.recvChan)
	t.Assert(len(got), Equals, 1)
	gotConfig := &agent.Config{}
	if err := json.Unmarshal(got[0].Data, gotConfig); err != nil {
		t.Fatal(err)
	}

	/**
	 * Verify new agent config in memory.
	 */
	expect := *s.config
	expect.ApiKey = "101"
	expect.Links = nil
	if ok, diff := test.IsDeeply(gotConfig, &expect); !ok {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	/**
	 * Verify new agent config in API connector.
	 */
	t.Check(s.api.ApiKey(), Equals, "101")
	t.Check(s.api.Hostname(), Equals, agent.DEFAULT_API_HOSTNAME)

	/**
	 * Verify new agent config on disk.
	 */
	data, err = ioutil.ReadFile(s.configFile)
	t.Assert(err, IsNil)
	gotConfig = &agent.Config{}
	if err := json.Unmarshal(data, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotConfig, &expect); !same {
		// @todo: if expect is not ptr, IsDeeply dies with "got ptr, expected struct"
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	// After changing the API key, the agent's ws should NOT reconnect yet,
	// but status should show that its link has changed, so sending a Reconnect
	// cmd will cause agent to reconnect its ws.
	gotCalled := test.WaitTrace(s.client.TraceChan)
	expectCalled := []string{"Start", "Connect"}
	t.Check(gotCalled, DeepEquals, expectCalled)
}

func (s *AgentTestSuite) TestSetConfigApiHostname(t *C) {
	newConfig := *s.config
	newConfig.ApiHostname = "http://localhost"
	data, err := json.Marshal(newConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "SetConfig",
		Service: "agent",
		Data:    data,
	}
	s.sendChan <- cmd

	got := test.WaitReply(s.recvChan)
	t.Assert(len(got), Equals, 1)
	gotConfig := &agent.Config{}
	if err := json.Unmarshal(got[0].Data, gotConfig); err != nil {
		t.Fatal(err)
	}

	/**
	 * Verify new agent config in memory.
	 */
	expect := *s.config
	expect.ApiHostname = "http://localhost"
	expect.Links = nil
	if ok, diff := test.IsDeeply(gotConfig, &expect); !ok {
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	/**
	 * Verify new agent config in API connector.
	 */
	t.Check(s.api.Hostname(), Equals, "http://localhost")
	t.Check(s.api.ApiKey(), Equals, "789")

	/**
	 * Verify new agent config on disk.
	 */
	data, err = ioutil.ReadFile(s.configFile)
	t.Assert(err, IsNil)
	gotConfig = &agent.Config{}
	if err := json.Unmarshal(data, gotConfig); err != nil {
		t.Fatal(err)
	}
	if same, diff := test.IsDeeply(gotConfig, &expect); !same {
		// @todo: if expect is not ptr, IsDeeply dies with "got ptr, expected struct"
		t.Logf("%+v", gotConfig)
		t.Error(diff)
	}

	// After changing the API host, the agent's ws should NOT reconnect yet,
	// but status should show that its link has changed, so sending a Reconnect
	// cmd will cause agent to reconnect its ws.
	gotCalled := test.WaitTrace(s.client.TraceChan)
	expectCalled := []string{"Start", "Connect"}
	t.Check(gotCalled, DeepEquals, expectCalled)

	/**
	 * Test Reconnect here since it's usually done after changing ApiHostname/
	 */

	// There is NO reply after reconnect because we can't recv cmd on one connection
	// and reply on another.  Instead, we should see agent try to reconnect:
	connectChan := make(chan bool)
	s.client.SetConnectChan(connectChan)
	defer s.client.SetConnectChan(nil)

	cmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "Reconnect",
		Service: "agent",
	}
	s.sendChan <- cmd

	// Wait for agent to reconnect.
	<-connectChan
	connectChan <- true

	gotCalled = test.WaitTrace(s.client.TraceChan)
	expectCalled = []string{"Disconnect", "Connect"}
	t.Check(gotCalled, DeepEquals, expectCalled)
}

func (s *AgentTestSuite) TestKeepalive(t *C) {
	newConfig := *s.config
	newConfig.Keepalive = 1 // 1s
	data, err := json.Marshal(newConfig)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "SetConfig",
		Service: "agent",
		Data:    data,
	}
	s.sendChan <- cmd

	reply := test.WaitReply(s.recvChan)
	t.Assert(reply, HasLen, 1)
	t.Assert(reply[0].Error, Equals, "")
	t.Assert(reply[0].Cmd, Equals, "SetConfig") // is reply to SetConfig not a Pong...

	// Agent should be sending a Pong every 1s now which is sent as a
	// reply to no cmd (it's a platypus).
	<-time.After(2 * time.Second)
	reply = test.WaitReply(s.recvChan)
	if len(reply) < 1 {
		t.Fatal("No Pong recieved")
	}
	t.Check(reply[0].Cmd, Equals, "Pong")

	// Restore Keepalive to high value so DrainSendChan() in TearDownTest()
	// doesn't keep receiving Pongs (on fast machine it should be able to drain
	// chan between Pongs but on slow test machine maybe not).
	data, err = json.Marshal(s.config)
	t.Assert(err, IsNil)
	cmd = &proto.Cmd{
		Ts:      time.Now(),
		User:    "daniel",
		Cmd:     "SetConfig",
		Service: "agent",
		Data:    data,
	}
	s.sendChan <- cmd
	reply = test.WaitReply(s.recvChan)
	t.Assert(reply, HasLen, 1)
	t.Assert(reply[0].Error, Equals, "")
	t.Assert(reply[0].Cmd, Equals, "SetConfig")
}
