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
	client proto.Client
	logChan chan *log.LogEntry
	serviceManagers map[string]service.Manager
	// --
	log *log.LogWriter
}

func NewAgent(config *Config, client proto.Client, logChan chan *log.LogEntry, serviceManagers map[string]service.Manager) *Agent {
	agent := &Agent{
		config: config,
		client: client,
		serviceManagers: serviceManagers,
		// --
		log: log.NewLogWriter(logChan, ""),
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
			agent.log.Error(err)
			continue
		}
		agent.log.Debug("Recv", msg)
		agent.handleMsg(msg)
	}
}

func (agent *Agent) handleMsg(m *proto.Msg) error {
	cmdDone := make(chan error)
	go func() {
		var err error
		switch {
		case m.Cmd == "update-config":
			agent.updateConfig(m.Data)
		case m.Cmd == "update-agent":
			agent.updateAgent(m.Data)
		case m.Cmd == "set-log-level":
			agent.setLogLevel(m.Data)
		case m.Cmd == "ping":
			agent.ping()
		case m.Cmd == "status":
			agent.status(m.Data)
		case m.Cmd == "shutdown":
			agent.shutdown(m.Data)
		case m.Cmd == "start-service":
			s := new(msg.StartService)
			if err := json.Unmarshal(m.Data, s); err == nil {
				err = agent.handleStartService(s)
			}
		case m.Cmd == "stop-service":
			agent.stopService(m.Data)
		case m.Cmd == "update-service":
			agent.updateService(m.Data)
		case m.Cmd == "pause-sending-data":
			agent.pauseSendingData(m.Data)
		case m.Cmd == "resume-sending-data":
			agent.resumeSendingData(m.Data)
		default:
			// error, unknown command
		}
		cmdDone <- err
	}()

	// Wait 1 minute for the cmd to complete, else return an error.
	cmdTimeout := time.After(time.Minute)
	for {
		select {
		case err := <-cmdDone:
			return err
		case <-time.After(500 * time.Millisecond):
			// @todo send waiting msg to server
		case <-cmdTimeout:
			return CmdTimeoutError{Cmd:m.Cmd}
		}
	}

	// @todo shouldn't reach here, but if we do, log an error
}

/////////////////////////////////////////////////////////////////////////////
// proto.Msg.Cmd handlers
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) updateConfig(data []byte) {
}

func (agent *Agent) updateAgent(data []byte) {
}

func (agent *Agent) setLogLevel(data []byte) {
}

func (agent *Agent) ping() error {
	agent.log.Debug("Agent.ping")
	agent.client.Send(proto.Pong())
	return nil
}

func (agent *Agent) status(data []byte) {
}

func (agent *Agent) shutdown(data []byte) {
	agent.stopAllServices()
	os.Exit(0)
}

func (agent *Agent) handleStartService(s *msg.StartService) error {
	agent.log.Debug("Agent.handleStartService")

	// Check that we have a manager for the service.
	m, ok := agent.serviceManagers[s.Name]
	if !ok {
		return UnknownServiceError{Service:s.Name}
	}

	// Return error if service is running.  To keep things simple,
	// we do not restart the service or verifty that the given config
	// matches the running config.  Only stopped services can be started.
	if m.IsRunning() {
		return service.ServiceIsRunningError{Service:s.Name}
	}

	// Start the service with the given config.
	err := m.Start(s.Config)
	return err
}

func (agent *Agent) stopService(data []byte) {
}

func (agent *Agent) updateService(data []byte) {
}

func (agent *Agent) pauseSendingData(data []byte) {
}

func (agent *Agent) resumeSendingData(data []byte) {
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
