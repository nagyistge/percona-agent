/*
    Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/log"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/ticker"
	"io/ioutil"
	golog "log"
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
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
}

func main() {
	t0 := time.Now()

	/**
	 * Agent config (require API key and agent UUID)
	 */

	cmd, arg := ParseCmdLine()

	// Check that config file exists.
	configFile := agent.DEFAULT_CONFIG_FILE // default
	if cmd == "start" && arg != "" {
		// percona-agent <config file>
		configFile = arg
	}
	if !pct.FileExists(configFile) {
		golog.Fatalf("Agent config file %s does not exist", configFile)
	}

	configDir :=  filepath.Dir(configFile)
	agentConfig, _ := agent.LoadConfig(configDir)

	// Make sure agent config has everything we need.
	if valid, missing := CheckConfig(agentConfig, configFile); !valid {
		golog.Printf("%s is missing %d settings: %s", configFile, len(missing), strings.Join(missing, ", "))
		os.Exit(-1)
	}

	golog.Printf("AgentUuid: %s\n", agentConfig.AgentUuid)

	/**
	 * Ping and exit, maybe.
	 */

	if cmd == "ping" {
		if arg == "" {
			arg = agentConfig.ApiHostname
		}
		golog.Println("Ping " + arg + "...")
		t0 := time.Now()
		ok, resp := Ping(agentConfig.ApiKey, arg)
		d := time.Now().Sub(t0)
		golog.Printf("%+v\n", resp)
		if !ok {
			golog.Printf("Ping FAIL (%s)", d)
			os.Exit(1)
		} else {
			golog.Printf("Ping OK (%s)", d)
			os.Exit(0)
		}
	}

	/**
	 * PID file
	 */

	if agentConfig.PidFile != "" {
		if err := WritePidFile(agentConfig.PidFile); err != nil {
			golog.Fatalln(err)
		}
		defer pct.RemoveFile(agentConfig.PidFile)
	}
	golog.Println("PidFile: " + agentConfig.PidFile)

	/**
	 * RESTful entry links
	 */

	var links map[string]string
	if len(agentConfig.Links) == 0 {
		var err error
		if links, err = GetLinks(agentConfig.ApiKey, "https://"+agentConfig.ApiHostname); err != nil {
			golog.Fatalln(err)
		}
	}

	/**
	 * Agent auth credentials (for websocket connections)
	 */

	auth, origin := MakeAgentAuth(agentConfig)

	/**
	 * Log relay
	 */

	// Log websocket client, possibly disabled later.
	logClient, err := client.NewWebsocketClient(nil, links["log"], origin, auth)
	if err != nil {
		golog.Fatalln(err)
	}
	logManager := log.NewManager(logClient)
	logConfig, err  := logManager.LoadConfig(configDir)
	if err := logManager.Start(&proto.Cmd{}, logConfig); err != nil {
		golog.Panicf("Error starting log service: ", err)
	}
	logChan := logManager.Relay().LogChan()

	/**
	 * Data spooler and sender
	 */

	dataClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "data-sender-ws"), links["data"], origin, auth)
	dataManager := data.NewManager(
		pct.NewLogger(logChan, "data"),
		auth.Hostname,
		dataClient,
	)
	dataConfig, err := dataManager.LoadConfig(configDir)
	if err := dataManager.Start(&proto.Cmd{}, dataConfig); err != nil {
		golog.Panicf("Error starting data service: ", err)
	}

	/**
	 * Collecct/report ticker (master clock)
	 */

	nowFunc := func() int64 { return time.Now().UTC().UnixNano() }
	clock := ticker.NewRolex(&ticker.EvenTickerFactory{}, nowFunc)

	/**
	 * Metric monitors
	 */

	mmManager := mm.NewManager(
		pct.NewLogger(logChan, "mm"),
		&monitor.MonitorFactory{logManager.Relay().LogChan()},
		clock,
		dataManager.Spooler(),
	)
	mmConfig, err  := mmConfig.LoadConfig(configDir)
	if err := mmManager.Start(&proto.Cmd{}, nil); err != nil {
		golog.Panicf("Error starting mm service: ", err)
	}
	startMonitors(mmManager)

	/**
	 * Query Analytics
	 */

	qanManager := qan.NewManager(
		pct.NewLogger(logChan, "qan"),
		&mysql.Connection{},
		clock,
		&qan.FileIntervalIterFactory{},
		&qan.SlowLogWorkerFactory{},
		dataManager.Spooler(),
	)
	qanConfig, err  := qan.LoadConfig(configDir)
	if err := dataManager.Start(&proto.Cmd{}, dataConfig); err != nil {
		golog.Panicf("Error starting data service: ", err)
	}


	/**
	 * Agent
	 */

	cmdClient, err := client.NewWebsocketClient(
		pct.NewLogger(logChan, "agent-cmd-ws"),
		links["cmd"],
		origin,
		auth,
	)
	if err != nil {
		golog.Fatal(err)
	}

	services := map[string]pct.ServiceManager{
		"log":  logManager,
		"data": dataManager,
		"qan":  qanManager,
		"mm":   mmManager,
	}

	agent := agent.NewAgent(
		agentConfig,
		auth,
		pct.NewLogger(logChan, "agent"),
		cmdClient,
		services,
	)

	t := time.Now().Sub(t0)
	golog.Printf("Running agent (%s)\n", t)

	stopReason, update := agent.Run()

	golog.Printf("stopReason=%s, update=%t\n", stopReason, update)
}

func ParseCmdLine() (cmd, arg string) {
	usage := "Usage: percona-agent command [arg]\n\n" +
	    "Commands:\n"+
		"  help                   Print help and exit\n" +
		"  ping    [API hostname] Ping API, requires API key\n" +
		"  start   [config file]  Start agent\n" +
		"  version                Print version and exist\n\n"+
		"Defaults:\n"+
		"  API hostname  " + agent.DEFAULT_CONFIG_FILE + "\n"+
		"  config file   " + agent.DEFAULT_API_HOSTNAME + "\n"
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


func GetLinks(apiKey, url string) (map[string]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		golog.Fatal(err)
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	golog.Println("Getting entry links from", url)

	backoff := pct.NewBackoff(5 * time.Minute)
	for {
		time.Sleep(backoff.Wait())

		resp, err := client.Do(req)
		if err != nil {
			golog.Printf("GET %s error: client.Do: %s", url, err)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			golog.Printf("GET %s error: ioutil.ReadAll: %s", url, err)
			continue
		}

		if resp.StatusCode >= 400 {
			golog.Printf("Error %d from %s\n", resp.StatusCode, url)
		} else if len(body) == 0 {
			golog.Println("OK response from ", url, "but no content")
		}

		links := &proto.Links{}
		if err := json.Unmarshal(body, links); err != nil {
			golog.Printf("GET %s error: json.Unmarshal: %s: %s", url, err, string(body))
			continue
		}

		if err := CheckLinks(links.Links); err != nil {
			golog.Println(err)
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

func StartMonitors(configDir string, m pct.Manager) error {
	configFiles, err := filepath.Glob(configDir + "/*-monitor.conf")
	if err != nil {
		golog.Fatal(err)
	}

	for _, configFile := range configFiles {
		filename := filepath.Base(configFile)
		config, err := ioutil.ReadFile(configFile)
		if err != nil {
			golog.Log(err)
			continue
		}
		cmd := &proto.Cmd{
			Ts:      time.Now().UTC(),
			User:    "percona-agent",
			Service: "mm",
			Cmd:     "StartService",
			Data:    config,
		}
		reply := mm.Handle(cmd)
		if reply.Error != "" {
			golog.Log(reply.Error)
		}
	}
}

func Ping(apiKey, url string) (bool, *http.Response) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		golog.Printf("Ping %s error: http.NewRequest: %s", url, err)
		return false, nil
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		golog.Printf("Ping %s error: client.Do: %s", url, err)
		return false, resp
	}
	_, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		golog.Printf("Ping %s error: ioutil.ReadAll: %s", url, err)
		return false, resp
	}

	if resp.StatusCode == 200 {
		return true, resp
	}

	return false, resp
}
