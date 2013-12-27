package main


import (
	"os"
	"os/user"
	"fmt"
	"flag"
	golog "log"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-tools/agent"
	pctlog "github.com/percona/cloud-tools/log"
)

const (
	API_HOSTNAME = "cloud-api.percona.com"
	LOG_FILE     = "/var/log/pct-agentd.log"
	LOG_WS       = "/log/agent/write"
	AGENT_WS     = "/cmd/agent"
)

// There are 3 configs, in asc order of precedence: /etc config files,
// env vars, cmd line.
var config agent.Config

func init() {
	flag.StringVar(&config.ApiKey,      "api-hostname", API_HOSTNAME, "API hostname")
	flag.StringVar(&config.ApiKey,      "api-key",      "",           "API key")
	flag.StringVar(&config.AgentUuid,   "agent-uuid",   "",           "Agent UUID")
	flag.StringVar(&config.LogFilename, "log-file",     LOG_FILE,     "Log file")
	flag.StringVar(&config.PidFilename, "pid-file",     ""            "PID file")
}

func main() {
	// Parse command line into config.
	flag.Parse()

	// PID file must not exist if given.
	if config.PidFile != "" {
		if err := writePidFile(newConfig.PidFile); err != nil {
			golog.Fatalln(err)
		}
		defer removeFile(config.PidFile)
	}

	// Read agent UUID from file if not given.
	if config.AgentUuid = "" {
		uuid, err := GetAgentUuid(AGENT_UUID_FILE)
		if err != nil {
			golog.Fatalf("No agent UUID: -agent-uuid not given and %s\n", err)
		}
		config.AgentUuid = uuid
	}

	u, _ := user.Current()
	hostname, _ := os.Hostname()
	username, _ := u.Username
	origin := "http://" + username + "@" + hostname

	// Start the central logger.  It doesn't stop until we stop.
	logClient, err := client.NewWebsocketClient("ws://" + config.ApiHostname + LOG_WS, origin)
	if err != nil {
		golog.Fatalln(err)
	}
	logChan := make(chan *proto.LogEntry, 100)
	logFile, err := pctlog.OpenLogFile(config.LogFile)
	if err != nil {
		golog.Fatalln(err)
	}
	logRelay := pctlog.NewLogRelay(logClient, logChan, logFile)
	go logRelay.Run()

	// Create and run the agent.
	auth := &proto.AgentAuth{
		ApiKey: config.ApiKey,
		Uuid: config.AgentUuid,
		Hostname: hostname,
		Username: username,
	}

	logger := pct.NewLogger(logChan, "agent")

	agentClient, err := client.NewWebsocketClient("ws://" + config.ApiHostname + AGENT_WS, origin)
	if err != nil {
		golog.Fatalln(err)
	}

	services := map[string]*pct.ServiceManager{
		"qan": nil,
		"mm": nil,
	}

	agent := agent.NewAgent(auth, logRelay, logger, agentClient, services)
	stopReason, upgrade := agent.Run()
}

func (agent *Agent) writePidFile(pidFile string) error {
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(pidFile, flags, 0644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	if err != nil {
		return err
	}
	err = file.Close()
	return err
}

func removeFile (file string) error {
	if file != "" {
		err := os.Remove(file)
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
