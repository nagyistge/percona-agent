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
	Links map[string]string
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	/**
	 * Bootstrap the most basic components.  If all goes well,
	 * we can create and start the agent.
	 */

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

	// Make sure config has everything we need.
	if valid, missing := CheckConfig(config, configFile); !valid {
		log.Println("Invalid config:")
		for _, m := range missing {
			log.Printf("  - %s\n", m)
		}
		os.Exit(-1)
	}

	// Check for and create PID file.
	if config.PidFile != "" {
		if err := WritePidFile(config.PidFile); err != nil {
			log.Fatalln(err)
		}
		defer removeFile(config.PidFile)
	}

	// Get entry links from API.  This only requires an API key.
	httpClient := client.NewHttpClient(config.ApiKey)
	links, err := GetLinks(httpClient, config.ApiHostname)
	if err != nil {
		log.Fatalf("Unable to get links: %s", err)
	}
	config.Links = links

	// Make a proto.AgentAuth so we can connect websockets.
	auth, origin := MakeAgentAuth(config)

	// Start the log relay (sends pct.Logger log entries to API and/or log file).
	logLevel := proto.LogLevels[config.LogLevel]
	var logRelay *logrelay.LogRelay
	if config.Disabled("LogFile") {
		log.Println("LogFile disabled")
		logRelay = logrelay.NewLogRelay(nil, config.LogFile, logLevel)
	} else {
		logLink, exist := config.Links["log"]
		if !exist || logLink == "" {
			log.Fatalf("Unable to get log link")
		}
		logClient, err := client.NewWebsocketClient(logLink, origin, auth)
		if err != nil {
			log.Fatalf("Unable to create log websocket connection (link: %s): %s", logLink, err)
		}
		logRelay = logrelay.NewLogRelay(logClient, config.LogFile, logLevel)
	}
	go logRelay.Run()

	/**
	 * Create and start the agent.
	 */

	logger := pct.NewLogger(logRelay.LogChan(), "agent")

	cmdLink, exist := config.Links["cmd"]
	if !exist || cmdLink == "" {
		log.Fatalf("Unable to get cmd link")
	}
	cmdClient, err := client.NewWebsocketClient(cmdLink, origin, auth)
	if err != nil {
		log.Fatalf("Unable to create cmd websocket connection (link: %s): %s", cmdLink, err)
	}

	services := map[string]pct.ServiceManager{
		"qan": nil,
		"mm":  nil,
	}

	agent := agent.NewAgent(config, auth, logRelay, logger, cmdClient, services)
	agent.Run()
}

func CheckConfig(config *agent.Config, configFile string) (bool, []string) {
	isValid := true
	missing := []string{}

	if config.ApiHostname == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No ApiHostname in %s\n", configFile)) // shouldn't happen
	}
	if config.ApiKey == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No ApiKey in %s\n", configFile))
	}
	if config.AgentUuid == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No AgentUuid in %s\n", configFile))
	}
	return isValid, missing
}

func GetLinks(client pct.HttpClient, link string) (map[string]string, error) {
	links := &apiLinks{}
	if err := client.Get(link, links, time.Hour*24*7); err != nil {
		return nil, err
	}
	return links.Links, nil
}

func WritePidFile(pidFile string) error {
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

func MakeAgentAuth(config *agent.Config) (*proto.AgentAuth, string) {
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
	return auth, origin
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
