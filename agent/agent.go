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

package agent

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	golog "log"
	"sync"
	"time"
)

const (
	CMD_QUEUE_SIZE    = 10
	STATUS_QUEUE_SIZE = 10
	MAX_ERRORS        = 3
)

type Agent struct {
	config    *Config
	configMux  *sync.RWMutex
	configDir string
	logger    *pct.Logger
	client    pct.WebsocketClient
	services  map[string]pct.ServiceManager
	// --
	cmdSync *pct.SyncChan
	cmdChan chan *proto.Cmd
	//
	statusSync *pct.SyncChan
	status     *pct.Status
	statusChan chan *proto.Cmd
	//
	stopping   bool
	stopReason string
	update     bool
	//
	cmdHandlerSync    *pct.SyncChan
	statusHandlerSync *pct.SyncChan
	stopChan          chan bool
}

func NewAgent(config *Config, logger *pct.Logger, client pct.WebsocketClient, services map[string]pct.ServiceManager) *Agent {
	agent := &Agent{
		config:   config,
		configMux: &sync.RWMutex{},
		logger:   logger,
		client:   client,
		services: services,
		// --
		status:     pct.NewStatus([]string{"agent", "agent-cmd-handler", "agent-api"}),
		cmdChan:    make(chan *proto.Cmd, CMD_QUEUE_SIZE),
		statusChan: make(chan *proto.Cmd, STATUS_QUEUE_SIZE),
		stopChan:   make(chan bool, 1),
	}
	return agent
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// percona-agent:@goroutine[0]
func (agent *Agent) Run() (stopReason string, update bool) {
	logger := agent.logger
	logger.Debug("start")

	// Reset for testing.
	agent.stopping = false
	agent.update = false

	// Start client goroutines for sending/receving cmd/reply via channels
	// so we can do non-blocking send/recv.  This only needs to be done once.
	// The chans are buffered, so they work for awhile if not connected.
	client := agent.client
	client.Start()
	cmdChan := client.RecvChan()
	replyChan := client.SendChan()

	go agent.connect()

	defer func() {
		logger.Info("Agent stopped")
		client.Disconnect()
		client.Stop()
	}()

	/*
	 * Start the status and cmd handlers.  Most messages must be serialized because,
	 * for example, handling start-service and stop-service at the same
	 * time would cause weird problems.  The cmdChan serializes messages,
	 * so it's "first come, first serve" (i.e. fifo).  Concurrency has
	 * consequences: e.g. if user1 sends a start-service and it succeeds
	 * and user2 send the same start-service, user2 will get a ServiceIsRunningError.
	 * Status requests are handled concurrently so the user can always see what
	 * the agent is doing even if it's busy processing commands.
	 */
	agent.cmdHandlerSync = pct.NewSyncChan()
	go agent.cmdHandler()

	agent.statusHandlerSync = pct.NewSyncChan()
	go agent.statusHandler()

	// Allow those ^ goroutines to crash up to MAX_ERRORS.  Any more and it's
	// probably a code bug rather than  bad input, network error, etc.
	cmdHandlerErrors := 0
	statusHandlerErrors := 0

	agent.status.Update("agent", "Ready")
	logger.Info("Ready")
AGENT_LOOP:
	for {
		logger.Debug("wait")
		select {
		case cmd := <-cmdChan: // from API
			if cmd.Cmd == "Abort" {
				// Try to log the abort, but this cmd should be fail-safe so don't wait too long.
				go agent.logger.Fatal("ABORT: %s", cmd)
				time.Sleep(3 * time.Second)
				golog.Panicf("%s\n", cmd)
			}

			logger.Debug("recv: ", cmd)

			if agent.stopping {
				// Already received Stop or Update, so reject further cmds.
				logger.Info("Got stop again, ignorning")
				err := pct.CmdRejectedError{Cmd: cmd.Cmd, Reason: agent.stopReason}
				replyChan <- cmd.Reply(nil, err)
				continue AGENT_LOOP
			}

			switch cmd.Cmd {
			case "Stop", "Update":
				agent.stopping = true
				var reason string
				if cmd.Cmd == "Stop" {
					reason = fmt.Sprintf("agent received Stop command [%s]", cmd)
				} else {
					reason = fmt.Sprintf("agent received Update command [%s]", cmd)
					update = true
				}
				logger.Info("STOP: " + reason)
				agent.stopReason = reason
				go agent.stop(cmd)
			case "Status":
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.statusChan <- cmd: // to statusHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "statusQueue", Size: STATUS_QUEUE_SIZE}
					replyChan <- cmd.Reply(nil, err)
				}
			default:
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.cmdChan <- cmd: // to cmdHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "cmdQueue", Size: CMD_QUEUE_SIZE}
					replyChan <- cmd.Reply(nil, err)
				}
			}
		case <-agent.cmdHandlerSync.CrashChan:
			cmdHandlerErrors++
			if cmdHandlerErrors < MAX_ERRORS {
				logger.Error("cmdHandler crashed, restarting")
				go agent.cmdHandler()
			} else {
				logger.Fatal("Too many cmdHandler errors")
				// todo: return or exit?
			}
		case <-agent.statusHandlerSync.CrashChan:
			statusHandlerErrors++
			if statusHandlerErrors < MAX_ERRORS {
				logger.Error("statusHandler crashed, restarting")
				go agent.statusHandler()
			} else {
				logger.Fatal("Too many statusHandler errors")
				// todo: return or exit?
			}
		case err := <-client.ErrorChan():
			logger.Warn(err)
		case connected := <-client.ConnectChan():
			if connected {
				logger.Info("Connected to API")
				cmdHandlerErrors = 0
				statusHandlerErrors = 0
			} else {
				// websocket closed/crashed/err
				logger.Warn("Lost connection to API")
				if agent.stopping {
					logger.Warn("Lost connection to API while stopping")
				}
				go agent.connect()
			}
		case <-agent.stopChan:
			break AGENT_LOOP
		} // select

		agent.status.Update("agent", "Ready")

	} // for AGENT_LOOP

	return agent.stopReason, agent.update
}

// @goroutine[0]
func (agent *Agent) connect() {
	agent.status.Update("agent", "Connecting to API")
	agent.logger.Info("Connecting to API")
	agent.client.Connect()
}

// @goroutine[0]
func (agent *Agent) stop(cmd *proto.Cmd) {
	agent.logger.InResponseTo(cmd)
	defer agent.logger.InResponseTo(nil)

	agent.logger.Info("Stopping cmdHandler")
	agent.status.UpdateRe("agent", "Stopping cmdHandler", cmd)
	agent.cmdHandlerSync.Stop()
	agent.cmdHandlerSync.Wait()

	for service, manager := range agent.services {
		agent.logger.Info("Stopping " + service)
		agent.status.UpdateRe("agent", "Stopping "+service, cmd)
		manager.Stop(cmd)
	}

	agent.logger.Info("Stopping statusHandler")
	agent.status.UpdateRe("agent", "Stopping statusHandler", cmd)
	agent.statusHandlerSync.Stop()
	agent.statusHandlerSync.Wait()

	agent.logger.Info("Stopping agent")
	agent.status.UpdateRe("agent", "Stopping agent", cmd)
	agent.stopChan <- true

	agent.logger.Info("Agent stopped")
	agent.status.UpdateRe("agent", "Stopped", cmd)
}

func (m *Agent) WriteConfig(config interface{}, name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	m.logger.Info("Writing", file)
	return pct.WriteConfig(file, config)
}

func (m *Agent) RemoveConfig(name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	m.logger.Info("Removing", file)
	return pct.RemoveFile(file)
}

// --------------------------------------------------------------------------
// Command handler
// --------------------------------------------------------------------------

// Run:@goroutine[1]
func (agent *Agent) cmdHandler() {
	replyChan := agent.client.SendChan()
	cmdReply := make(chan *proto.Reply, 1)

	// defer is LIFO, so send done signal last.
	defer func() {
		agent.status.Update("agent-cmd-handler", "Stopped")
		agent.cmdHandlerSync.Done()
	}()

	for {
		agent.status.Update("agent-cmd-handler", "Idle")

		select {
		case cmd := <-agent.cmdChan:
			agent.status.UpdateRe("agent-cmd-handler", "Running", cmd)

			// Handle the cmd in a separate goroutine so if it gets stuck it won't affect us.
			go func() {
				var reply *proto.Reply
				if cmd.Service == "agent" {
					reply = agent.Handle(cmd)
				} else {
					if manager, ok := agent.services[cmd.Service]; ok {
						reply = manager.Handle(cmd)
					} else {
						reply = cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
					}
				}
				cmdReply <- reply
			}()

			// Wait for the cmd to complete.
			var reply *proto.Reply
			select {
			case reply = <-cmdReply:
				// todo: instrument cmd exec time
			case <-time.After(20 * time.Second):
				reply = cmd.Reply(nil, pct.CmdTimeoutError{Cmd: cmd.Cmd})
			}

			select {
			case replyChan <- reply:
			case <-time.After(20 * time.Second):
				agent.logger.Warn("Failed to send reply:", reply)
			}
		case <-agent.cmdHandlerSync.StopChan: // from stop()
			agent.cmdHandlerSync.Graceful()
			return
		}
	}
}

// cmdHandler:@goroutine[3]
func (agent *Agent) Handle(cmd *proto.Cmd) *proto.Reply {
	agent.status.UpdateRe("agent-cmd-handler", "Running", cmd)
	agent.logger.Info("Running", cmd)

	defer func() {
		agent.logger.Info("Done running", cmd)
	}()

	var data interface{}
	var err error
	switch cmd.Cmd {
	case "StartService":
		data, err = agent.handleStartService(cmd)
	case "StopService":
		data, err = agent.handleStopService(cmd)
	case "GetConfig":
		data, err = agent.handleGetConfig(cmd)
	case "SetConfig":
		data, err = agent.handleSetConfig(cmd)
	default:
		err = pct.UnknownCmdError{Cmd: cmd.Cmd}
	}

	if err != nil {
		agent.logger.Error(err)
	}

	return cmd.Reply(data, err)
}

// Handle:@goroutine[3]
func (agent *Agent) handleStartService(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "StartService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name and config.
	s := &proto.ServiceData{}
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return nil, err
	}

	// Check if we have a manager for the service.
	m, ok := agent.services[s.Name]
	if !ok {
		return nil, pct.UnknownServiceError{Service: s.Name}
	}

	// Start the service with the given config.
	if err := m.Start(cmd, s.Config); err != nil {
		return nil, err
	}

	return nil, nil
}

// Handle:@goroutine[3]
func (agent *Agent) handleStopService(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "StopService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name.
	s := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return nil, err
	}

	// Check if we have a manager for the service.  If not, that's ok,
	// just return because the service can't be running if we don't have it.
	m, ok := agent.services[s.Name]
	if !ok {
		return nil, pct.UnknownServiceError{Service: s.Name}
	}

	// Stop the service.
	err := m.Stop(cmd)
	return nil, err
}

// Handle:@goroutine[3]
func (agent *Agent) handleGetConfig(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "GetConfig", cmd)
	agent.logger.Info(cmd)

	agent.configMux.RLock()
	defer agent.configMux.RUnlock()

	config := agent.config
	config.Links = nil  // not saved, not reported

	return config, nil
}

// Handle:@goroutine[3]
func (agent *Agent) handleSetConfig(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "SetConfig", cmd)
	agent.logger.Info(cmd)

	newConfig := &Config{}
	if err := json.Unmarshal(cmd.Data, newConfig); err != nil {
		return nil, err
	}

	agent.configMux.Lock()
	defer agent.configMux.Unlock()

	finalConfig := agent.config

	if newConfig.ApiKey != "" && newConfig.ApiKey != agent.config.ApiKey {
		// todo: test access with new API key?
		agent.logger.Warn("Changing API key from", agent.config.ApiKey, "to", newConfig.ApiKey)
		finalConfig.ApiKey = newConfig.ApiKey
	}

	// todo: change ApiHostname and PidFile

	if err := agent.WriteConfig(finalConfig, "agent"); err != nil {
		agent.logger.Error("New agent config not applied")
		return nil, err
	}

	agent.config = finalConfig
	return finalConfig, nil
}

//---------------------------------------------------------------------------
// Status handler
// --------------------------------------------------------------------------

// Run:@goroutine[2]
func (agent *Agent) statusHandler() {
	replyChan := agent.client.SendChan()

	defer agent.statusHandlerSync.Done()

	// Status handler doesn't have its own status because that's circular,
	// e.g. "How am I? I'm good!".

	for {
		select {
		case cmd := <-agent.statusChan:
			switch cmd.Service {
			case "":
				replyChan <- cmd.Reply(agent.AllStatus())
			case "agent":
				replyChan <- cmd.Reply(agent.Status())
			default:
				if manager, ok := agent.services[cmd.Service]; ok {
					replyChan <- manager.Handle(cmd)
				} else {
					replyChan <- cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
				}
			}
		case <-agent.statusHandlerSync.StopChan:
			agent.statusHandlerSync.Graceful()
			return
		}
	}
}

// statusHandler:@goroutine[2]
func (agent *Agent) Status() map[string]string {
	return agent.status.All()
}

// statusHandler:@goroutine[2]
func (agent *Agent) AllStatus() map[string]string {
	status := agent.status.All()
	for service, manager := range agent.services {
		if manager == nil { // should not happen
			status[service] = fmt.Sprintf("ERROR: %s service manager is nil", service)
			continue
		}
		for k, v := range manager.Status() {
			status[k] = v
		}
	}
	return status
}
