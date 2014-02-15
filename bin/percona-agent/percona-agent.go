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
	"path/filepath"
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
	cmd, arg := ParseCmdLine()

	// Check that config file exists.
	configFile := agent.CONFIG_FILE // default
	if cmd == "start" && arg != "" {
		// percona-agent <config file>
		configFile = arg
	}
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Fatalf("Agent config file %s does not exist", configFile)
	}

	// Create default config.
	config := &agent.Config{
		ApiHostname: agent.API_HOSTNAME,
		LogDir:      agent.LOG_DIR,
		LogFile:     agent.LOG_FILE,
		LogLevel:    agent.LOG_LEVEL,
		DataDir:     agent.DATA_DIR,
		ConfigDir:   filepath.Dir(configFile),
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
	 * Ping and exit, maybe.
	 */

	if cmd == "ping" {
		if arg == "" {
			arg = config.ApiHostname
		}
		log.Println("Ping " + arg + "...")
		t0 := time.Now()
		ok, resp := Ping(config.ApiKey, arg)
		d := time.Now().Sub(t0)
		log.Printf("%+v\n", resp)
		if !ok {
			log.Printf("Ping FAIL (%s)", d)
			os.Exit(1)
		} else {
			log.Printf("Ping OK (%s)", d)
			os.Exit(0)
		}
	}

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
		if links, err = GetLinks(config.ApiKey, "https://"+config.ApiHostname); err != nil {
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
	var logFile string
	if config.Disabled("LogFile") {
		log.Println("LogFile: DISABLED")
		logFile = ""
	} else {
		if config.LogFile == "STDOUT" || config.LogFile == "STDERR" {
			logFile = config.LogFile
		} else {
			logFile = config.LogDir + "/" + config.LogFile
		}
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
	 * Data spooler and sender
	 */

	var sz data.Serializer
	if config.Disabled("gzip") {
		sz = data.NewJsonSerializer()
	} else {
		sz = data.NewJsonGzipSerializer()
	}
	dataSpooler := data.NewDiskvSpooler(
		pct.NewLogger(logRelay.LogChan(), "data-spooler"),
		config.DataDir,
		sz,
		auth.Hostname,
	)
	if err := dataSpooler.Start(); err != nil {
		log.Fatalln("Cannot start data spooler:", err)
	}

	dataClient, err := client.NewWebsocketClient(pct.NewLogger(logRelay.LogChan(), "data-sender-ws"), links["data"], origin, auth)
	if err != nil {
		log.Fatalln(err)
	}
	dataSender := data.NewSender(
		pct.NewLogger(logRelay.LogChan(), "data-sender"),
		dataClient,
		dataSpooler,
		time.Tick(30*time.Second), // todo
	)
	if err := dataSender.Start(); err != nil {
		log.Fatalln("Cannot start data sender:", err)
	}

	/**
	 * Create and start the agent.
	 */

	cmdClient, err := client.NewWebsocketClient(
		pct.NewLogger(logRelay.LogChan(), "agent-cmd-ws"),
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
		dataSpooler,
	)

	monitors := map[string]mm.Monitor{
		"mysql": mysqlMonitor.NewMonitor(pct.NewLogger(logRelay.LogChan(), "mysql-monitor")),
	}
	mmManager := mm.NewManager(
		pct.NewLogger(logRelay.LogChan(), "mm"),
		monitors,
		clock,
		dataSpooler,
	)

	services := map[string]pct.ServiceManager{
		"qan": qanManager,
		"mm":  mmManager,
	}

	agentLogger := pct.NewLogger(logRelay.LogChan(), "agent")
	agent := agent.NewAgent(config, auth, logRelay, agentLogger, cmdClient, services)

	// Start previously configured, running services.
	startServiceCmds := LoadServiceConfigs(config.ConfigDir)
	if len(startServiceCmds) > 0 {
		t := time.Now().Sub(t0)
		log.Printf("Starting services (%s)\n", t)
		agent.StartServices(startServiceCmds)
	}

	// Start agent.
	t := time.Now().Sub(t0)
	log.Printf("Running agent (%s)\n", t)
	stopReason, update := agent.Run()

	// todo:
	log.Printf("stopReason=%s, update=%t\n", stopReason, update)
}

func ParseCmdLine() (cmd, arg string) {
	usage := "Usage: percona-agent command [arg]\n\n" +
	    "Commands:\n"+
		"  help                   Print help and exit\n" +
		"  ping    [API hostname] Ping API, requires API key\n" +
		"  start   [config file]  Start agent\n" +
		"  version                Print version and exist\n\n"+
		"Defaults:\n"+
		"  API hostname  " + agent.CONFIG_FILE + "\n"+
		"  config file   " + agent.API_HOSTNAME + "\n"
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(1)
	}
	cmd = os.Args[1]
	switch cmd {
	case "version":
		fmt.Printf("percona-agent %s\n", VERSION)
		os.Exit(0)
	case "help":
		fmt.Println(usage)
		os.Exit(0)
	case "ping", "start":
		if len(os.Args) > 3 {
			fmt.Println(cmd + " takes only one arg")
			fmt.Println(usage)
			os.Exit(1)
		} else if len(os.Args) == 3 {
			arg = os.Args[2]
		}
	default:
		fmt.Println("Unknown command: " + cmd)
		fmt.Println(usage)
		os.Exit(-1)
	}
	return cmd, arg
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

	log.Println("Getting entry links from", url)

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

		if resp.StatusCode >= 400 {
			log.Printf("Error %d from %s\n", resp.StatusCode, url)
		} else if len(body) == 0 {
			log.Println("OK response from ", url, "but no content")
		}

		links := &proto.Links{}
		if err := json.Unmarshal(body, links); err != nil {
			log.Printf("GET %s error: json.Unmarshal: %s: %s", url, err, string(body))
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

func LoadServiceConfigs(configDir string) []*proto.Cmd {
	configFiles, err := filepath.Glob(configDir + "/*.conf")
	if err != nil {
		log.Fatal(err)
	}

	configs := []*proto.Cmd{}
	for _, configFile := range configFiles {
		filename := filepath.Base(configFile)
		if filename == "agent.conf" {
			continue
		}
		var cmd *proto.Cmd
		switch filename {
		case qan.CONFIG_FILE:
			cmd = makeStartServiceCmd("agent", "qan", configFile)
		case mm.CONFIG_FILE:
			cmd = makeStartServiceCmd("agent", "mm", configFile)
		case mysqlMonitor.CONFIG_FILE:
			cmd = makeStartServiceCmd("mm", "mysql", configFile)
		default:
			log.Fatal("Unknown config file:", configFile)
		}
		// todo: mm needs to start before monitors
		// tood: monitors can't start without mm
		configs = append(configs, cmd)
	}
	return configs
}

func makeStartServiceCmd(manager, service, configFile string) *proto.Cmd {
	config, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	serviceData := &proto.ServiceData{
		Name:   service,
		Config: config,
	}
	data, err := json.Marshal(serviceData)
	if err != nil {
		log.Fatal(err)
	}
	cmd := &proto.Cmd{
		Ts:      time.Now().UTC(),
		Service: manager,
		Cmd:     "StartService",
		Data:    data,
	}
	return cmd
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

func Ping(apiKey, url string) (bool, *http.Response) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Ping %s error: http.NewRequest: %s", url, err)
		return false, nil
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Ping %s error: client.Do: %s", url, err)
		return false, resp
	}
	_, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("Ping %s error: ioutil.ReadAll: %s", url, err)
		return false, resp
	}

	if resp.StatusCode == 200 {
		return true, resp
	}

	return false, resp
}
