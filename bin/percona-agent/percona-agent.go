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
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/log"
	"github.com/percona/cloud-tools/mm"
	mmMonitor "github.com/percona/cloud-tools/mm/monitor"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"github.com/percona/cloud-tools/sysconfig"
	sysconfigMonitor "github.com/percona/cloud-tools/sysconfig/monitor"
	"github.com/percona/cloud-tools/ticker"
	"io/ioutil"
	golog "log"
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

	golog.Println("AgentUuid: " + agentConfig.AgentUuid)

	/**
	 * Ping and exit, maybe.
	 */

	if cmd == "ping" {
		t0 := time.Now()
		ok, resp := pct.PingAPI(agentConfig.ApiHostname, agentConfig.ApiKey)
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

	pidFile := pct.NewPidFile()
	if err := pidFile.Set(agentConfig.PidFile); err != nil {
		golog.Fatalln(err)
	}
	defer pidFile.Remove()
	golog.Println("PidFile: " + agentConfig.PidFile)

	/**
	 * REST API
	 */

	api, err := ConnectAPI(agentConfig)
	if err != nil {
		golog.Fatal(err)
	}

	/**
	 * Log relay
	 */

	logChan := make(chan *proto.LogEntry, log.BUFFER_SIZE*3)

	// Log websocket client, possibly disabled later.
	logClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "log-ws"), api, "log")
	if err != nil {
		golog.Fatalln(err)
	}
	logManager := log.NewManager(logClient, logChan)
	logConfig, err := logManager.LoadConfig(configDir)
	if err := logManager.Start(&proto.Cmd{}, logConfig); err != nil {
		golog.Panicf("Error starting log service: %s", err)
	}

	/**
	 * Service instance manager
	 */

	itManager := instance.NewManager(
		pct.NewLogger(logChan, "instance-manager"),
		configDir,
	)
	if err := itManager.Start(nil, nil); err != nil {
		golog.Panicf("Error starting instance manager: ", err)
	}

	/**
	 * Data spooler and sender
	 */

	hostname, _ := os.Hostname()

	dataClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "data-ws"), api, "data")
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
	clock := ticker.NewClock(&ticker.RealTickerFactory{}, nowFunc)

	/**
	 * Metric and system config monitors
	 */

	mmManager := mm.NewManager(
		pct.NewLogger(logChan, "mm"),
		mmMonitor.NewFactory(logChan, itManager.Repo()),
		clock,
		dataManager.Spooler(),
		itManager.Repo(),
	)
	mmConfig, err := mmManager.LoadConfig(configDir)
	if err := mmManager.Start(&proto.Cmd{}, mmConfig); err != nil {
		golog.Panicf("Error starting mm service: ", err)
	}
	StartMonitors("mm", configDir, configDir+"/mm-*.conf", mmManager)

	sysconfigManager := sysconfig.NewManager(
		pct.NewLogger(logChan, "sysconfig"),
		sysconfigMonitor.NewFactory(logChan, itManager.Repo()),
		clock,
		dataManager.Spooler(),
		itManager.Repo(),
	)
	sysconfigConfig, err := sysconfigManager.LoadConfig(configDir)
	if err := sysconfigManager.Start(&proto.Cmd{}, sysconfigConfig); err != nil {
		golog.Panicf("Error starting sysconfig service: ", err)
	}
	StartMonitors("sysconfig", configDir, configDir+"/sysconfig-*.conf", sysconfigManager)

	/**
	 * Query Analytics
	 */

	qanManager := qan.NewManager(
		pct.NewLogger(logChan, "qan"),
		&mysql.RealConnectionFactory{},
		clock,
		&qan.FileIntervalIterFactory{},
		&qan.SlowLogWorkerFactory{},
		dataManager.Spooler(),
		itManager.Repo(),
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

	cmdClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "agent-ws"), api, "cmd")
	if err != nil {
		golog.Fatal(err)
	}

	services := map[string]pct.ServiceManager{
		"log":  logManager,
		"data": dataManager,
		"qan":  qanManager,
		"mm":   mmManager,
		"it":   itManager,
	}

	agent := agent.NewAgent(
		agentConfig,
		pidFile,
		pct.NewLogger(logChan, "agent"),
		api,
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

func ConnectAPI(agentConfig *agent.Config) (*pct.API, error) {
	golog.Println("ApiHostname: " + agentConfig.ApiHostname)
	golog.Println("ApiKey: " + agentConfig.ApiKey)

	api := pct.NewAPI()
	backoff := pct.NewBackoff(5 * time.Minute)
	t0 := time.Now()
	for time.Now().Sub(t0) < time.Hour*24*7 {
		time.Sleep(backoff.Wait())
		golog.Println("Connecting to API")
		if err := api.Connect(agentConfig.ApiHostname, agentConfig.ApiKey, agentConfig.AgentUuid); err != nil {
			golog.Println(err)
			continue
		}
		golog.Println("Connected to API")
		return api, nil // success
	}

	return nil, errors.New("Timeout connecting to " + agentConfig.ApiHostname)
}

func StartMonitors(service, configDir, glob string, manager pct.ServiceManager) error {
	configFiles, err := filepath.Glob(glob)
	if err != nil {
		golog.Fatal(err)
	}

	for _, configFile := range configFiles {
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			golog.Println("Read " + configFile + ": " + err.Error())
			continue
		}
		config := &mm.Config{}
		json.Unmarshal(data, config)
		cmd := &proto.Cmd{
			Ts:      time.Now().UTC(),
			User:    "percona-agent",
			Service: service,
			Cmd:     "StartService",
			Data:    data,
		}
		golog.Println("Starting " + service + " monitor: " + configFile)
		reply := manager.Handle(cmd)
		if reply.Error != "" {
			golog.Println("Start " + configFile + " monitor:" + reply.Error)
		}
	}
	return nil
}
