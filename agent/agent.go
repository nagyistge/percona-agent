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
	"strings"
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
	configDir string
	logger    *pct.Logger
	client    pct.WebsocketClient
	services  map[string]pct.ServiceManager
	// --
	cmdSync *pct.SyncChan
	cmdChan chan *proto.Cmd
	cmdq    []*proto.Cmd
	cmdqMux *sync.RWMutex
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
		logger:   logger,
		client:   client,
		services: services,
		// --
		cmdq:       make([]*proto.Cmd, CMD_QUEUE_SIZE),
		cmdqMux:    new(sync.RWMutex),
		status:     pct.NewStatus([]string{"Agent", "AgentCmdHandler"}),
		cmdChan:    make(chan *proto.Cmd, CMD_QUEUE_SIZE),
		statusChan: make(chan *proto.Cmd, STATUS_QUEUE_SIZE),
		stopChan:   make(chan bool, 1),
	}
	return agent
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
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

	agent.status.Update("Agent", "Ready")
	logger.Info("Ready")
AGENT_LOOP:
	for {
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
				agent.status.UpdateRe("Agent", "Queueing", cmd)
				select {
				case agent.statusChan <- cmd: // to statusHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "statusQueue", Size: STATUS_QUEUE_SIZE}
					replyChan <- cmd.Reply(nil, err)
				}
			default:
				agent.status.UpdateRe("Agent", "Queueing", cmd)
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

		agent.status.Update("Agent", "Ready")

	} // for AGENT_LOOP

	return agent.stopReason, agent.update
}

// @goroutine[0]
func (agent *Agent) connect() {
	agent.status.Update("Agent", "Connecting to API")
	agent.logger.Info("Connecting to API")
	agent.client.Connect()
}

// @goroutine[0]
func (agent *Agent) stop(cmd *proto.Cmd) {
	agent.status.UpdateRe("Agent", "Stopping cmdHandler", cmd)
	agent.cmdHandlerSync.Stop()
	agent.cmdHandlerSync.Wait()

	for service, manager := range agent.services {
		agent.status.UpdateRe("Agent", "Stopping "+service, cmd)
		manager.Stop(cmd)
	}

	agent.status.UpdateRe("Agent", "Stopping statusHandler", cmd)
	agent.statusHandlerSync.Stop()
	agent.statusHandlerSync.Wait()

	agent.status.UpdateRe("Agent", "Stopping agent", cmd)
	agent.stopChan <- true
	agent.status.UpdateRe("Agent", "Stopped", cmd)
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

// @goroutine[1]
func (agent *Agent) cmdHandler() {
	replyChan := agent.client.SendChan()

	// defer is LIFO, so send done signal last.
	defer agent.cmdHandlerSync.Done()
	defer agent.status.Update("AgentCmdHandler", "Stopped")

	for {
		agent.status.Update("AgentCmdHandler", "Ready")
		select {
		case <-agent.cmdHandlerSync.StopChan: // from stop()
			agent.cmdHandlerSync.Graceful()
			return
		case cmd := <-agent.cmdChan:
			reply := agent.handleCmd(cmd)
			if reply.Error != "" {
				agent.logger.Warn(reply.Error)
			}
			agent.status.UpdateRe("AgentCmdHandler", "Replying", cmd)
			replyChan <- reply
		}
	}
}

// @goroutine[0:1]
func (agent *Agent) handleCmd(cmd *proto.Cmd) *proto.Reply {
	agent.status.UpdateRe("AgentCmdHandler", "Running", cmd)
	agent.logger.Info("Running", cmd)

	agent.cmdqMux.Lock()
	agent.cmdq = append(agent.cmdq, cmd)
	agent.cmdqMux.Unlock()

	defer func() {
		// Pop the cmd from the queue.
		agent.status.UpdateRe("AgentCmdHandler", "Finishing", cmd)
		agent.cmdqMux.Lock()
		agent.cmdq = agent.cmdq[0 : len(agent.cmdq)-1]
		agent.cmdqMux.Unlock()
		agent.status.Update("AgentCmdHandler", "Idle")
	}()

	// Run the command in another goroutine so we can wait for it
	// (and possibly timeout) in this goroutine.
	cmdReply := make(chan *proto.Reply, 1)
	go func() {
		var data interface{}
		var reply *proto.Reply
		defer func() { cmdReply <- reply }()

		if cmd.Service == "agent" {
			// Agent command
			var err error
			switch cmd.Cmd {
			case "StartService":
				err = agent.handleStartService(cmd)
			case "StopService":
				err = agent.handleStopService(cmd)
			case "GetConfig":
				data = agent.config
			default:
				// todo: handle SetConfig
				err = pct.UnknownCmdError{Cmd: cmd.Cmd}
			}
			reply = cmd.Reply(data, err)
		} else {
			// Service command
			if manager, ok := agent.services[cmd.Service]; ok {
				reply = manager.Handle(cmd)
			} else {
				reply = cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
			}
		}
	}()

	// Wait for the cmd to complete.
	var reply *proto.Reply
	select {
	case reply = <-cmdReply:
		// todo: instrument cmd exec time
	case <-time.After(20 * time.Second):
		reply = cmd.Reply(nil, pct.CmdTimeoutError{Cmd: cmd.Cmd})
	}

	return reply
}

func (agent *Agent) handleStartService(cmd *proto.Cmd) error {
	agent.status.UpdateRe("AgentCmdHandler", "StartService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name and config.
	s := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return err
	}

	// Check if we have a manager for the service.
	m, ok := agent.services[s.Name]
	if !ok {
		return pct.UnknownServiceError{Service: s.Name}
	}

	// Start the service with the given config.
	if err := m.Start(cmd, s.Config); err != nil {
		return err
	}

	return nil
}

func (agent *Agent) handleStopService(cmd *proto.Cmd) error {
	agent.status.UpdateRe("AgentCmdHandler", "StopService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name.
	s := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return err
	}

	// Check if we have a manager for the service.  If not, that's ok,
	// just return because the service can't be running if we don't have it.
	m, ok := agent.services[s.Name]
	if !ok {
		return nil
	}

	// Stop the service.
	err := m.Stop(cmd)
	return err
}

//---------------------------------------------------------------------------
// Status handler
// --------------------------------------------------------------------------

// @goroutine[2]
func (agent *Agent) statusHandler() {
	replyChan := agent.client.SendChan()

	defer agent.statusHandlerSync.Done()

	// Status handler doesn't update agent.status because that's circular,
	// e.g. "How am I? I'm good!".

	for {
		select {
		case <-agent.statusHandlerSync.StopChan:
			agent.statusHandlerSync.Graceful()
			return
		case cmd := <-agent.statusChan:
			switch cmd.Service {
			case "":
				// Summary status of all services.
				replyChan <- cmd.Reply(agent.Status())
			case "agent":
				// Internal status of agent.
				replyChan <- cmd.Reply(agent.InternalStatus())
			default:
				// Internal status of another service.
				if manager, ok := agent.services[cmd.Service]; ok {
					replyChan <- manager.Handle(cmd)
				} else {
					replyChan <- cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
				}
			}
		}
	}
}

func (agent *Agent) Status() map[string]string {
	return agent.status.Get("Agent", true)

	for service, manager := range agent.services {
		if manager == nil { // should not happen
			status[service] = fmt.Sprintf("ERROR: %s service manager is nil", service)
			continue
		}
		status[service] = manager.Status()
	}
}

func (agent *Agent) InternalStatus() proto.StatusData {
	status := agent.status.All()

	agent.cmdqMux.RLock()
	defer agent.cmdqMux.RUnlock()
	cmds := []string{}
	for n, cmd := range agent.cmdq {
		if cmd != nil {
			cmds = append(cmds, fmt.Sprintf("[%n] %s", n, cmd.String()))
		}
	}
	status["AgentCmdQueue"] = strings.Join(cmds, "\n")

	return status
}
