package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/logrelay"
	"github.com/percona/cloud-tools/mm"
	mysqlMonitor "github.com/percona/cloud-tools/mm/mysql"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/ticker"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"
)

const (
	VERSION = "1.0.0"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
}

func main() {
	t0 := time.Now()

	/**
	 * Agent config
	 */

	// Parse command line.
	configFile := ParseCmdLine()

	// Check that config file exists.
	if configFile == "" {
		configFile = agent.CONFIG_DIR + "/agent.conf"
	}
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Fatalf("Agent config file %s does not exist", configFile)
	}

	// Create default config.
	config := &agent.Config{
		ApiHostname: agent.API_HOSTNAME,
		LogDir:      agent.LOG_DIR,
		LogLevel:    agent.LOG_LEVEL,
		DataDir:     agent.DATA_DIR,
	}

	// Overwrite default config with config file.
	if err := config.Apply(agent.LoadConfig(configFile)); err != nil {
		log.Fatal(err)
	}

	// Make sure config has everything we need.
	if valid, missing := CheckConfig(config, configFile); !valid {
		log.Printf("%s is missing %d settings: %s", configFile, len(missing), strings.Join(missing, ", "))
		os.Exit(-1)
	}

	// Make the dirs we need, if they don't already exist.
	if err := MakeDirs([]string{config.LogDir, config.DataDir}); err != nil {
		log.Fatalln(err)
	}

	log.Printf("AgentUuid: %s\n", config.AgentUuid)
	log.Printf("DataDir: %s\n", config.DataDir)

	/**
	 * PID file
	 */

	if config.PidFile != "" {
		if err := WritePidFile(config.PidFile); err != nil {
			log.Fatalln(err)
		}
		defer removeFile(config.PidFile)
	}
	log.Println("PidFile: " + config.PidFile)

	/**
	 * RESTful entry links
	 */

	var links map[string]string
	if len(config.Links) == 0 {
		var err error
		if links, err = GetLinks(config.ApiKey, "http://"+config.ApiHostname); err != nil {
			log.Fatalln(err)
		}
	}

	/**
	 * Agent auth credentials
	 */

	auth, origin := MakeAgentAuth(config)

	/**
	 * Log relay
	 */

	// Log websocket client, possibly disabled later.
	logClient, err := client.NewWebsocketClient(nil, links["log"], origin, auth)
	if err != nil {
		log.Fatalln(err)
	}

	// Log file, if not disabled.
	logFile := config.LogDir + "/agent.log"
	if config.Disabled("LogFile") {
		log.Println("LogFile: DISABLED")
		logFile = ""
	} else {
		log.Printf("LogFile: %s\n", logFile)
	}

	// Log level (should already be validated).
	logLevel := proto.LogLevelNumber[config.LogLevel]

	// The log relay with option client and file.
	logRelay := logrelay.NewLogRelay(logClient, logFile, logLevel)

	// Update logger for log ws client now that log relay exists.
	if logClient != nil {
		logClient.SetLogger(pct.NewLogger(logRelay.LogChan(), "agent-ws-log"))
	}

	// todo: fix this hack
	if config.Disabled("LogApi") {
		logRelay.Offline(true)
		log.Println("LogApi: DISABLED")
	}

	go logRelay.Run()

	/**
	 * Master clock
	 */

	nowFunc := func() int64 { return time.Now().UTC().UnixNano() }
	clock := ticker.NewRolex(&ticker.EvenTickerFactory{}, nowFunc)

	/**
	 * Data spooler
	 */

	spool := data.NewDiskvSpooler(
		pct.NewLogger(logRelay.LogChan(), "spooler"),
		config.DataDir,
		data.NewJsonGzipSerializer(),
		auth.Hostname,
	)
	if err := spool.Start(); err != nil {
		log.Fatalln("Cannot start spooler:", err)
	}

	/**
	 * Create and start the agent.
	 */

	cmdClient, err := client.NewWebsocketClient(
		pct.NewLogger(logRelay.LogChan(), "agent-ws-cmd"),
		links["cmd"],
		origin,
		auth,
	)
	if err != nil {
		log.Fatal(err)
	}

	qanManager := qan.NewManager(
		pct.NewLogger(logRelay.LogChan(), "qan"),
		&mysql.Connection{},
		clock,
		&qan.FileIntervalIterFactory{},
		&qan.SlowLogWorkerFactory{},
		spool,
	)

	monitors := map[string]mm.Monitor{
		"mysql": mysqlMonitor.NewMonitor(pct.NewLogger(logRelay.LogChan(), "mysql-monitor")),
	}
	mmManager := mm.NewManager(
		pct.NewLogger(logRelay.LogChan(), "mm"),
		monitors,
		clock,
		spool,
	)

	services := map[string]pct.ServiceManager{
		"qan": qanManager,
		"mm":  mmManager,
	}

	agentLogger := pct.NewLogger(logRelay.LogChan(), "agent")
	agent := agent.NewAgent(config, auth, logRelay, agentLogger, cmdClient, services)

	t := time.Now().Sub(t0)
	log.Printf("Running agent (%s)\n", t)
	agent.Run()
}

func ParseCmdLine() string {
	arg := ""
	usage := "Usage: percona-agent [<config file>|help|version]"
	if len(os.Args) == 2 {
		arg = os.Args[1]
		switch arg {
		case "version":
			fmt.Printf("percona-agent %s\n", VERSION)
			os.Exit(0)
		case "help":
			fmt.Println(usage)
			os.Exit(0)
		default:
			fmt.Printf("Invalid arg: %s\n", arg)
			fmt.Println(usage)
			os.Exit(-1)
		}
	} else if len(os.Args) > 2 {
		fmt.Println("Unknown extra args: ", os.Args)
		fmt.Println(usage)
		os.Exit(-1)
	}
	return arg
}

func CheckConfig(config *agent.Config, configFile string) (bool, []string) {
	isValid := true
	missing := []string{}

	if config.ApiHostname == "" {
		isValid = false
		missing = append(missing, "ApiHostname")
	}
	if config.ApiKey == "" {
		isValid = false
		missing = append(missing, "ApiKey")
	}
	if config.AgentUuid == "" {
		isValid = false
		missing = append(missing, "AgentUuid")
	}
	return isValid, missing
}

func MakeDirs(dirs []string) error {
	for _, dir := range dirs {
		if err := os.Mkdir(dir, 0775); err != nil {
			if !os.IsExist(err) {
				return err
			}
		}
	}
	return nil
}

func GetLinks(apiKey, url string) (map[string]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	backoff := pct.NewBackoff(5 * time.Minute)
	for {
		time.Sleep(backoff.Wait())

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("GET %s error: client.Do: %s", url, err)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("GET %s error: ioutil.ReadAll: %s", url, err)
			continue
		}

		links := &proto.Links{}
		if err := json.Unmarshal(body, links); err != nil {
			log.Printf("GET %s error: json.Unmarshal: %s", url, err)
			continue
		}

		if err := CheckLinks(links.Links); err != nil {
			log.Println(err)
			continue
		}

		return links.Links, nil
	}
}

func CheckLinks(links map[string]string) error {
	requiredLinks := []string{"cmd", "log", "data"}
	for _, link := range requiredLinks {
		logLink, exist := links[link]
		if !exist || logLink == "" {
			return errors.New("Missing " + link + " link")
		}
	}
	return nil
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
