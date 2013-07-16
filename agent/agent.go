package agent

import (
	"os"
	"os/user"
	"fmt"
	"github.com/percona/percona-cloud-tools/agent/config"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type Agent struct {
	Uuid string
	Config *config.Config
}

func (agent *Agent) Run(client *proto.Client) error {

	client.Connect()

	helloData := agent.GetHelloData()
	helloMsg := proto.NewMsg("hello", helloData)
	if err := client.Send(helloMsg); err != nil {
	}

	return nil
}

func (agent *Agent) GetHelloData() map[string] string {
	var data map[string]string
	u, _ := user.Current()
	data["agent_uuid"] = agent.Uuid
	data["hostname"], _ = os.Hostname()
	data["username"] = u.Username
	return data
}
