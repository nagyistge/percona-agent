package main


import (
	"fmt"
	"flag"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/ws"
/*
	"github.com/percona/percona-cloud-tools/agent/service"
	"github.com/percona/percona-cloud-tools/agent/data"
*/
)

const (
	API_ADDR = "wss://cloud-api.percona.com"
	AGENT_ENDPOINT = "/agent"
	AGENT_LOG_ENDPOINT = "/agent/log"
)

// There are 3 configs, in asc order of precedence: /etc config files,
// env vars, cmd line.
var cmdLineConfig agent.Config

func init() {
	flag.StringVar(&cmdLineConfig.ApiKey, "api-key", "", "API key")
	flag.StringVar(&cmdLineConfig.ApiKey, "api-addr", API_ADDR, "API address")
	flag.StringVar(&cmdLineConfig.AgentUuid, "agent-uuid", "", "Agent UUID")
	flag.StringVar(&cmdLineConfig.SpoolDir, "spool-dir", "/var/spool/percona-cloud-tools", "Agent data spool directory")
	flag.StringVar(&cmdLineConfig.LogFilename, "log-file", "/var/log/percona-cloud-tools-agent.log", "Agent log file")
	flag.StringVar(&cmdLineConfig.PidFilename, "pid-file", "/var/run/percona-cloud-tools-agent.pid", "percona-cloud-tools agent PID file")
	flag.StringVar(&cmdLineConfig.ConfigFilename, "config-file", "/etc/percona-cloud-tools/config", "Config file")
}

func main() {
	/*
	 * We need 3 things to run an agent: a config, a client, and a logger.
	 */

	// Parse command line into cmdLineConfig
	flag.Parse()

	// Parse -config-file into etcConfig
	etcConfig := new(config.Config)
	if err := etcConfig.ReadFile(cmdLineConfig.ConfigFilename); err != nil {
		panic(
			fmt.Sprintf("Error parsing -config-file %s: %s",
				cmdLineConfig.ConfigFilename, err))
	}

	// Create agentConfig from defaults and command line
	agentConfig := new(config.Config)
	agentConfig.Apply(etcConfig)
	agentConfig.Apply(&cmdLineConfig)
	fmt.Println(agentConfig)

	// Create a websocket client.  The Agent.Run() will connect it.
	wsClient := ws.NewClient(agentConfig.ApiUrl, AGENT_ENDPOINT)

	// Create a websocket logger.  This requires its own websocket client
	// so primary comm over that ^ ws client and the logger ws client are
	// independent and async.
	logWsClient := ws.NewClient(agentConfig.ApiUrl, AGENT_LOG_ENDPOINT)
	wsLogger := ws.NewLogger(logWsClient)

	// Create and run the agent.
	agent := new(agent.Agent)
	agent.Run()
}
