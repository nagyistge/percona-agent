package main

import (
	"fmt"
	"flag"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/config"
/*
	"github.com/percona/percona-cloud-tools/agent/service"
	"github.com/percona/percona-cloud-tools/agent/data"
*/
)

var cmdLineConfig config.Config

func init() {
	flag.StringVar(&cmdLineConfig.ApiKey, "api-key", "", "API key")
	flag.StringVar(&cmdLineConfig.AgentUuid, "agent-uuid", "", "Agent UUID")
	flag.StringVar(&cmdLineConfig.SpoolDir, "spool-dir", "/var/spool/percona-cloud-tools", "Agent data spool directory")
	flag.StringVar(&cmdLineConfig.LogFilename, "log-file", "/var/log/percona-cloud-tools-agent.log", "Agent log file")
	flag.StringVar(&cmdLineConfig.PidFilename, "pid-file", "/var/run/percona-cloud-tools-agent.pid", "percona-cloud-tools agent PID file")
	flag.StringVar(&cmdLineConfig.ConfigFilename, "config-file", "/etc/percona-cloud-tools/config", "Config file")
}

func main() {
	// Parse command line into cmdLineConfig
	flag.Parse()

	// Parse -config-file into defaultConfig
	defaultConfig := new(config.Config)
	if err := defaultConfig.ReadFile(cmdLineConfig.ConfigFilename); err != nil {
		panic(
			fmt.Sprintf("Error parsing -config-file %s: %s",
				cmdLineConfig.ConfigFilename, err))
	}

	// Create agentConfig from defaults and command line
	agentConfig := new(config.Config)
	agentConfig.Apply(defaultConfig)
	agentConfig.Apply(&cmdLineConfig)
	fmt.Println(agentConfig)

	agent := new(agent.Agent)
	agent.Config = agentConfig

	agent.Run()
}
