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
	"github.com/percona/cloud-tools/mm/monitor"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/ticker"
	"io/ioutil"
	golog "log"
	"net/http"
	"os"
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

	// Check that agent config file exists.
	configFile := agent.DEFAULT_CONFIG_FILE // default
	if cmd == "start" && arg != "" {
		// percona-agent <config file>
		configFile = arg
	}
	if !pct.FileExists(configFile) {
		golog.Fatalf("Agent config file %s does not exist", configFile)
	}

	agentConfig, err := agent.LoadConfig(configFile)
	if err != nil {
		golog.Panicf("Error loading "+configFile+": ", err)
	}

	// Make sure agent config has everything we need.
	if valid, missing := CheckConfig(agentConfig, configFile); !valid {
		golog.Printf("%s is missing %d settings: %s", configFile, len(missing), strings.Join(missing, ", "))
		os.Exit(-1)
	}

	// All service config files should be in same dir as agent config file.
	configDir := filepath.Dir(configFile)

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
		schema := "https://"
		if strings.HasPrefix(agentConfig.ApiHostname, "localhost") || strings.HasPrefix(agentConfig.ApiHostname, "127.0.0.1") {
			schema = "http://"
		}
		if links, err = GetAgentLinks(agentConfig.ApiKey, agentConfig.AgentUuid, schema+agentConfig.ApiHostname); err != nil {
			golog.Fatalln(err)
		}
	}

	hostname, _ := os.Hostname()
	origin := "http://" + hostname

	/**
	 * Log relay
	 */

	logChan := make(chan *proto.LogEntry, log.BUFFER_SIZE*3)

	// Log websocket client, possibly disabled later.
	logClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "log-ws"), links["log"], origin, agentConfig.ApiKey)
	if err != nil {
		golog.Fatalln(err)
	}
	logManager := log.NewManager(logClient, logChan)
	logConfig, err := logManager.LoadConfig(configDir)
	if err := logManager.Start(&proto.Cmd{}, logConfig); err != nil {
		golog.Panicf("Error starting log service: %s", err)
	}

	/**
	 * Data spooler and sender
	 */

	dataClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "data-ws"), links["data"], origin, agentConfig.ApiKey)
	if err != nil {
		golog.Fatalln(err)
	}
	dataManager := data.NewManager(
		pct.NewLogger(logChan, "data"),
		hostname,
		dataClient,
	)
	dataConfig, err := dataManager.LoadConfig(configDir)
	if err := dataManager.Start(&proto.Cmd{}, dataConfig); err != nil {
		golog.Panicf("Error starting data service: %s", err)
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
		monitor.NewFactory(logChan),
		clock,
		dataManager.Spooler(),
	)
	mmConfig, err := mmManager.LoadConfig(configDir)
	if mmConfig != nil {
		if err := mmManager.Start(&proto.Cmd{}, mmConfig); err != nil {
			golog.Panicf("Error starting mm service: ", err)
		}
	}
	StartMonitors(configDir, mmManager)

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
	qanConfig, err := qanManager.LoadConfig(configDir)
	if qanConfig != nil {
		if err := qanManager.Start(&proto.Cmd{}, qanConfig); err != nil {
			golog.Panicf("Error starting qan service: %s", err)
		}
	}

	/**
	 * Agent
	 */

	cmdClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "agent-ws"), links["cmd"], origin, agentConfig.ApiKey)
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
		"Commands:\n" +
		"  help                   Print help and exit\n" +
		"  ping    [API hostname] Ping API, requires API key\n" +
		"  start   [config file]  Start agent\n" +
		"  version                Print version and exist\n\n" +
		"Defaults:\n" +
		"  API hostname  " + agent.DEFAULT_CONFIG_FILE + "\n" +
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

func GetAgentLinks(apiKey, uuid, url string) (map[string]string, error) {
	golog.Println("Getting entry links from", url)
	backoff := pct.NewBackoff(5 * time.Minute)
	agentLink := ""
	for {
		time.Sleep(backoff.Wait())

		if agentLink == "" {
			if entryLinks, err := GetLinks(apiKey, url); err != nil {
				golog.Println(err)
				continue
			} else {
				var ok bool
				agentLink, ok = entryLinks["agents"]
				if !ok {
					golog.Println("No agents link")
					continue
				}
			}
		}

		agentLinks, err := GetLinks(apiKey, agentLink+"/"+uuid)
		if err != nil {
			golog.Println(err)
			continue
		}

		if err := CheckLinks(agentLinks); err != nil {
			golog.Println(err)
			continue
		}

		return agentLinks, nil // success
	}
}

func GetLinks(apiKey, url string) (map[string]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s error: client.Do: %s", url, err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("GET %s error: ioutil.ReadAll: %s", url, err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Error %d from %s\n", resp.StatusCode, url)
	} else if len(body) == 0 {
		return nil, fmt.Errorf("OK response from ", url, "but no content")
	}

	links := &proto.Links{}
	if err := json.Unmarshal(body, links); err != nil {
		return nil, fmt.Errorf("GET %s error: json.Unmarshal: %s: %s", url, err, string(body))
	}

	return links.Links, nil
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

func StartMonitors(configDir string, manager pct.ServiceManager) error {
	configFiles, err := filepath.Glob(configDir + "/*-monitor.conf")
	if err != nil {
		golog.Fatal(err)
	}

	for _, configFile := range configFiles {
		config, err := ioutil.ReadFile(configFile)
		if err != nil {
			golog.Println(err)
			continue
		}
		cmd := &proto.Cmd{
			Ts:      time.Now().UTC(),
			User:    "percona-agent",
			Service: "mm",
			Cmd:     "StartService",
			Data:    config,
		}
		reply := manager.Handle(cmd)
		if reply.Error != "" {
			golog.Println(reply.Error)
		}
	}
	return nil
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
