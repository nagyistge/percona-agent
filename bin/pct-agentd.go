package main

import (
	"fmt"
	"log"
	"os"
	"os/user"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/logrelay"
)

const (
	VERSION      = "1.0.0"
	CONFIG_FILE  = "/etc/percona/pct-agentd.conf"
	LOG_WS       = "/agent/log"
	AGENT_WS     = "/cmd/agent"
	QAN_DATA_URL = "/qan"
	MM_DATA_URL  = "/mm"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Parse command line.
	var arg string
	if len(os.Args) == 2 {
		arg = os.Args[1]
		switch arg {
		case "version":
			fmt.Printf("pct-agentd %s\n", VERSION)
			os.Exit(0)
		default:
			fmt.Printf("Invalid arg: %s\n", arg)
			os.Exit(-1)
		}
	} else if len(os.Args) > 2 {
		fmt.Println("Unknown extra args")
		os.Exit(-1)
	}

	// Create default config.
	config := &agent.Config{
		ApiHostname: "cloud-api.percona.com",
		LogFile: "/var/log/pct-agentd.log",
		LogLevel: "info",
	}

	// Overwrite default config with config file.
	configFile := arg
	if configFile == "" {
		configFile = CONFIG_FILE
	}
	if err := config.Apply(agent.LoadConfig(configFile)); err != nil {
		log.Fatal(err)
	}

	// Check for required config values.
	haveRequiredVals := true
	if config.ApiHostname == "" {
		fmt.Printf("No ApiHostname in %s\n", configFile) // shouldn't happen
		haveRequiredVals = false
	}
	if config.ApiKey == "" {
		fmt.Printf("No ApiKey in %s\n", configFile)
		haveRequiredVals = false
	}
	if config.AgentUuid == "" {
		log.Printf("No AgentUuid in %s\n", configFile)
		haveRequiredVals = false
	}
	if !haveRequiredVals {
		os.Exit(-1)
	}

	// Check/create PID file.
	if config.PidFile != "" {
		if err := writePidFile(config.PidFile); err != nil {
			log.Fatalln(err)
		}
		defer removeFile(config.PidFile)
	}

	// Get some values we'll need later.
	hostname, _ := os.Hostname()
	u, _ := user.Current()
	username := u.Username
	origin := "http://" + username + "@" + hostname

	auth := &proto.AgentAuth{
		ApiKey: config.ApiKey,
		Uuid: config.AgentUuid,
		Hostname: hostname,
		Username: username,
	}

	// Start the log relay (sends pct.Logger log entries to API and/or log file).
	logLevel := proto.LogLevels[config.LogLevel]
	var logRelay *logrelay.LogRelay
	if config.LogFileOnly {
		log.Println("LogFileOnly=true")
		logRelay = logrelay.NewLogRelay(nil, config.LogFile, logLevel)
	} else {
		wsClient, err := client.NewWebsocketClient("ws://" + config.ApiHostname + LOG_WS, origin, auth)
		if err != nil {
			log.Fatalln(err)
		}
		logRelay = logrelay.NewLogRelay(wsClient, config.LogFile, logLevel)
	}
	go logRelay.Run()

	// Create and run the agent.
	logger := pct.NewLogger(logRelay.LogChan(), "agent")

	agentClient, err := client.NewWebsocketClient("ws://" + config.ApiHostname + AGENT_WS, origin, auth)
	if err != nil {
		log.Fatalln(err)
	}

	services := map[string]pct.ServiceManager{
		"qan": nil,
		"mm": nil,
	}

	agent := agent.NewAgent(auth, logRelay, logger, agentClient, services)
	agent.Run()
}

func writePidFile(pidFile string) error {
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
