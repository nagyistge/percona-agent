package main

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/logrelay"
	"github.com/percona/cloud-tools/pct"
	"log"
	"os"
	"os/user"
	"time"
)

const (
	VERSION = "1.0.0"
)

type apiLinks struct {
	links map[string]string
}

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
		ApiHostname: agent.API_HOSTNAME,
		LogFile:     agent.LOG_FILE,
		LogLevel:    agent.LOG_LEVEL,
		DataDir:     agent.DATA_DIR,
	}

	// Overwrite default config with config file.
	configFile := arg
	if configFile == "" {
		configFile = agent.CONFIG_FILE
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

	// Get entry links: URLs for websockets, sending service data, etc.
	httpClient := client.NewHttpClient(config.ApiKey)
	links, err := GetLinks(httpClient, agent.API_HOSTNAME)
	if err != nil {
		log.Fatal(err)
	}
	config.Links = links

	// Get some values we'll need later.
	hostname, _ := os.Hostname()
	u, _ := user.Current()
	username := u.Username
	origin := "http://" + username + "@" + hostname

	auth := &proto.AgentAuth{
		ApiKey:   config.ApiKey,
		Uuid:     config.AgentUuid,
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
		wsClient, err := client.NewWebsocketClient(config.Links["log"], origin, auth)
		if err != nil {
			log.Fatalln(err)
		}
		logRelay = logrelay.NewLogRelay(wsClient, config.LogFile, logLevel)
	}
	go logRelay.Run()

	// Create and run the agent.
	logger := pct.NewLogger(logRelay.LogChan(), "agent")

	cmdClient, err := client.NewWebsocketClient(config.Links["cmd"], origin, auth)
	if err != nil {
		log.Fatalln(err)
	}

	services := map[string]pct.ServiceManager{
		"qan": nil,
		"mm":  nil,
	}

	agent := agent.NewAgent(config, auth, logRelay, logger, cmdClient, services)
	agent.Run()
}

func GetLinks(client pct.HttpClient, link string) (map[string]string, error) {
	links := &apiLinks{}
	if err := client.Get(link, links, time.Hour*24*7); err != nil {
		return nil, err
	}
	return links.links, nil
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

func removeFile(file string) error {
	if file != "" {
		err := os.Remove(file)
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
