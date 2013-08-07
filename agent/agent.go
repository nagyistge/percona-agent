package agent

import (
//	"os"
//	"os/user"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type Agent struct {
	Uuid string
	Services map[string]chan *proto.Msg
}
/*
func (agent *Agent) Run(config *Config, client *proto.Client, log *Logger) {

	// log.Info("Running agent", agent.Uuid)

	client.Connect()

	helloData := agent.GetHelloData()
	helloMsg := proto.NewMsg("hello", helloData)
	if err := client.Send(helloMsg); err != nil {
	}

	for msg := range recvChan {
		d.Dispatch(msg.Command, msg.Data)
	}
}

func (agent *Agent) GetHelloData() map[string] string {
	var data map[string]string
	u, _ := user.Current()
	data["agent_uuid"] = agent.Uuid
	data["hostname"], _ = os.Hostname()
	data["username"] = u.Username
	return data
}

func (agent *Agent) UpdateConfig() {
}

func (agent *Agent) Report() {
}

func (agent *Agent) SetLogLevel() {
}

func (agent *Agent) StopServices() {
}

func (agent *Agent) StartServices(data) {
	for serviceName := range data {
		if agent.RunningServices[serviceName] {
			agent.Services[serviceName] <- msg
		}
		c := make(chan *proto.Msg)
		sm := service.NewManager(serviceName, tasks, c)
		go sm.Run()
		agent.Services[serviceName] = c
	}
}

func (agent *Agent) StopSendingData() {
	for serviceName := range data {
		if agent.RunningServices[serviceName] {
			agent.Services[serviceName] <- msg
		}
	}
}

func (agent *Agent) Shutdown(data) {
	if data["abort"] {
		os.Exit(1)
	}
	agent.stopAllServices()
	os.Exit(0)
}

func (agent *Agent) Ping() {
	agent.Client.Sent(proto.NewMsg("pong", nil))
}

func (agent *Agent) SelfUpdate() {
	agent.stopAllServices()
	// todo
}

func (agent *Agent) stopAllServices() {
	stopMsg := proto.NewMsg("stop", nil)
	for serviceName := range agent.Services {
		agent.Services[serviceName] <- stopMsg
	}
}
*/
