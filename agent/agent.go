package agent

import (
	"os"
	"os/user"
	"time"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/proto/msg"
	"github.com/percona/percona-cloud-tools/agent/service"
)

type Agent struct {
	config *Config
	logRelay *log.LogRelayer
	log *log.LogWriter
	client proto.Client
	services map[string]service.Manager
}

func NewAgent(config *Config, logRelay *log.LogRelayer, log *log.LogWriter, client proto.Client, services map[string]service.Manager) *Agent {
	agent := &Agent{
		config: config,
		logRelay: logRelay,
		log: log,
		client: client,
		services: services,
	}
	return agent
}

func (agent *Agent) Run() {
	agent.log.Info("Running agent")

	agent.client.Connect()

	helloData := agent.getHelloData()
	helloMsg := proto.NewMsg("hello", helloData)
	if err := agent.client.Send(helloMsg); err != nil {
	}

	for {
		msg, err := agent.client.Recv()
		if err != nil {
			// error
			continue
		}
		agent.handleMsg(msg)
	}
}

func (agent *Agent) handleMsg(msg *proto.Msg) {
	cmdDone := make(chan bool)
	go func() {
		switch {
		case msg.Cmd == "update-config":
			agent.updateConfig(msg.Data)
		case msg.Cmd == "update-agent":
			agent.updateAgent(msg.Data)
		case msg.Cmd == "set-log-level":
			agent.setLogLevel(msg.Data)
		case msg.Cmd == "ping":
			agent.ping()
		case msg.Cmd == "status":
			agent.status(msg.Data)
		case msg.Cmd == "shutdown":
			agent.shutdown(msg.Data)
		case msg.Cmd == "start-service":
			agent.startService(msg.Data)
		case msg.Cmd == "stop-service":
			agent.stopService(msg.Data)
		case msg.Cmd == "update-service":
			agent.updateService(msg.Data)
		case msg.Cmd == "pause-sending-data":
			agent.pauseSendingData(msg.Data)
		case msg.Cmd == "resume-sending-data":
			agent.resumeSendingData(msg.Data)
		default:
			// error, unknown command
		}
		cmdDone <- true
	}()

	cmdTimeout := time.After(time.Minute)
	for {
		select {
			case <-cmdDone:
				break
			case <-time.After(500 * time.Millisecond):
				// send waiting msg to server
			case <-cmdTimeout:
				// send fail msg to server
				break
		}
	}
}

/////////////////////////////////////////////////////////////////////////////
// proto.Msg.Cmd handlers
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) updateConfig(data string) {
}

func (agent *Agent) updateAgent(data string) {
}

func (agent *Agent) setLogLevel(data string) {
}

func (agent *Agent) ping() {
	agent.client.Send(proto.Pong())
}

func (agent *Agent) status(data string) {
}

func (agent *Agent) shutdown(data string) {
	agent.stopAllServices()
	os.Exit(0)
}

func (agent *Agent) startService(data string) {
	s := new(msg.StartService)
	if err := json.Unmarshal(data, s); err != nil {
	}
	sm := agent.services[s.Name]
	if sm.IsRunning() {
		// error
		return
	}

	if err := sm.Start(s.Config); err != nil {
		// error
		return
	}

	// success
}

func (agent *Agent) stopService(data string) {
}

func (agent *Agent) updateService(data string) {
}

func (agent *Agent) pauseSendingData(data string) {
}

func (agent *Agent) resumeSendingData(data string) {
}

/////////////////////////////////////////////////////////////////////////////
// Internal methods
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) getHelloData() map[string] string {
	var data map[string]string
	u, _ := user.Current()
	//data["agent_uuid"] = agent.Uuid
	data["hostname"], _ = os.Hostname()
	data["username"] = u.Username
	return data
}

func (agent *Agent) stopAllServices() {
}
