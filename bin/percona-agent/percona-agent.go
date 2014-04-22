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
	"flag"
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
	golog "log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

const (
	VERSION = "1.0.0"
)

var (
	flagPing    bool
	flagBasedir string
	flagVersion bool
)

func init() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)

	flag.BoolVar(&flagPing, "ping", false, "Ping API")
	flag.StringVar(&flagBasedir, "basedir", pct.DEFAULT_BASEDIR, "Set basedir")
	flag.BoolVar(&flagVersion, "version", false, "Stop percona-agent")
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	if flagVersion {
		fmt.Printf("percona-agent %s\n", VERSION)
		os.Exit(0)
	}

	if err := pct.Basedir.Init(flagBasedir); err != nil {
		golog.Fatal(err)
	}

	/**
	 * Agent config (require API key and agent UUID)
	 */

	if !pct.FileExists(pct.Basedir.ConfigFile("agent")) {
		golog.Fatalf("Agent config file %s does not exist", pct.Basedir.ConfigFile("agent"))
	}

	bytes, err := agent.LoadConfig()
	if err != nil {
		golog.Fatalf("Invalid agent config: %s\n", err)
	}
	agentConfig := &agent.Config{}
	if err := json.Unmarshal(bytes, agentConfig); err != nil {
		golog.Panicf("Error parsing "+pct.Basedir.ConfigFile("agent")+": ", err)
	}

	golog.Println("ApiHostname: " + agentConfig.ApiHostname)
	golog.Println("AgentUuid: " + agentConfig.AgentUuid)

	/**
	 * Ping and exit, maybe.
	 */

	if flagPing {
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
	logManager := log.NewManager(
		logClient,
		logChan,
	)
	if err := logManager.Start(); err != nil {
		golog.Fatalf("Error starting logmanager: %s\n", err)
	}

	/**
	 * Service instance manager
	 */

	itManager := instance.NewManager(
		pct.NewLogger(logChan, "instance-manager"),
		pct.Basedir.Dir("config"),
		api,
	)
	if err := itManager.Start(); err != nil {
		golog.Fatalf("Error starting instance manager: %s\n", err)
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
		pct.Basedir.Dir("data"),
		hostname,
		dataClient,
	)
	if err := dataManager.Start(); err != nil {
		golog.Fatalf("Error starting data manager: %s\n", err)
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
	if err := mmManager.Start(); err != nil {
		golog.Fatalf("Error starting mm manager: %s\n", err)
	}

	sysconfigManager := sysconfig.NewManager(
		pct.NewLogger(logChan, "sysconfig"),
		sysconfigMonitor.NewFactory(logChan, itManager.Repo()),
		clock,
		dataManager.Spooler(),
		itManager.Repo(),
	)
	if err := sysconfigManager.Start(); err != nil {
		golog.Fatalf("Error starting sysconfig manager: %s\n", err)
	}

	/**
	 * Query Analytics
	 */

	qanManager := qan.NewManager(
		pct.NewLogger(logChan, "qan"),
		&mysql.RealConnectionFactory{},
		clock,
		qan.NewFileIntervalIterFactory(logChan),
		&qan.SlowLogWorkerFactory{},
		dataManager.Spooler(),
		itManager.Repo(),
	)
	if err := qanManager.Start(); err != nil {
		golog.Fatalf("Error starting qan manager: %s\n", err)
	}

	/**
	 * Signal handler
	 */

	// Generally the agent has a crash-only design, but QAN is so far the only service
	// which reconfigures MySQL: it enables the slow log, sets long_query_time, etc.
	// It's not terrible to leave slow log on, but it's nicer to turn it off.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		golog.Printf("Caught %s signal, shutting down...\n", sig)
		qanManager.Stop()
		os.Exit(1)
	}()

	/**
	 * Agent
	 */

	cmdClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "agent-ws"), api, "cmd")
	if err != nil {
		golog.Fatal(err)
	}

	// The official list of services known to the agent.  Adding a new service
	// requires a manager, starting the manager as above, and adding the manager
	// to this map.
	services := map[string]pct.ServiceManager{
		"log":       logManager,
		"data":      dataManager,
		"qan":       qanManager,
		"mm":        mmManager,
		"it":        itManager,
		"sysconfig": sysconfigManager,
	}

	agent := agent.NewAgent(
		agentConfig,
		pidFile,
		pct.NewLogger(logChan, "agent"),
		api,
		cmdClient,
		services,
	)

	update := agent.Run()
	golog.Printf("Agent stopped; update %t\n", update)

	qanManager.Stop() // see Signal handler ^

	if update {
		// todo
	}

	os.Exit(0)
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
