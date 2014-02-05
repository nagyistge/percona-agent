package agent

import (
	// Core
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
	// External
	"github.com/percona/cloud-protocol/proto"
	// Internal
	"github.com/percona/cloud-tools/logrelay"
	"github.com/percona/cloud-tools/pct"
)

const (
	CMD_QUEUE_SIZE    = 10
	STATUS_QUEUE_SIZE = 10
	DEFAULT_LOG_FILE  = "/var/log/pct-agentd.log"
	MAX_ERRORS        = 3
)

type Agent struct {
	config   *Config
	auth     *proto.AgentAuth
	logRelay *logrelay.LogRelay
	logger   *pct.Logger
	client   pct.WebsocketClient
	services map[string]pct.ServiceManager
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

func NewAgent(config *Config, auth *proto.AgentAuth, logRelay *logrelay.LogRelay, logger *pct.Logger, client pct.WebsocketClient, services map[string]pct.ServiceManager) *Agent {
	agent := &Agent{
		config:   config,
		auth:     auth,
		logRelay: logRelay,
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

	// Try and block forever to connect because the agent can't do anything
	// until connected.

	// Start client goroutines for sending/receving cmd/reply via channels
	// so we can do non-blocking send/recv.  This only needs to be done once.
	// The chans are buffered, so they work for awhile if not connected.
	client := agent.client
	client.Start()
	cmdChan := client.RecvChan()
	replyChan := client.SendChan()

	go agent.connect()

	defer func() {
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
			logger.Debug("recv:%s", cmd)

			if cmd.Cmd == "Abort" {
				// Try to log the abort, but this cmd should be fail-safe so don't wait too long.
				agent.logger.Fatal("ABORT: %s", cmd)
				time.Sleep(3 * time.Second)
				log.Panicf("%s\n", cmd)
			}

			if agent.stopping {
				// Already received Stop or Update, so reject further cmds.
				err := pct.CmdRejectedError{Cmd: cmd.Cmd, Reason: agent.stopReason}
				replyChan <- cmd.Reply(err, nil)
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
					replyChan <- cmd.Reply(err, nil)
				}
			default:
				agent.status.UpdateRe("Agent", "Queueing", cmd)
				select {
				case agent.cmdChan <- cmd: // to cmdHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "cmdQueue", Size: CMD_QUEUE_SIZE}
					replyChan <- cmd.Reply(err, nil)
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

func (agent *Agent) Status() map[string]string {
	return agent.status.All()
}

// @goroutine[0]
func (agent *Agent) connect() {
	agent.status.Update("Agent", "Connecting to API")
	agent.logger.Info("Connecting to API")
	agent.client.Connect()
	agent.logger.Info("Connected to API")
}

// @goroutine[0]
func (agent *Agent) stop(cmd *proto.Cmd) {
	agent.status.UpdateRe("Agent", "stopping cmdHandler", cmd)
	agent.cmdHandlerSync.Stop()
	agent.cmdHandlerSync.Wait()

	for service, manager := range agent.services {
		agent.status.UpdateRe("Agent", "stopping"+service, cmd)
		manager.Stop(cmd)
	}

	agent.status.UpdateRe("Agent", "stopping statusHandler", cmd)
	agent.statusHandlerSync.Stop()
	agent.statusHandlerSync.Wait()

	agent.status.UpdateRe("Agent", "stopping agent", cmd)
	agent.stopChan <- true
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
	agent.status.Update("AgentCmdHandler", "Ready")

	for {
		select {
		case <-agent.cmdHandlerSync.StopChan: // from stop()
			agent.cmdHandlerSync.Graceful()
			return
		case cmd := <-agent.cmdChan:
			agent.status.UpdateRe("AgentCmdHandler", "Running", cmd)

			agent.cmdqMux.Lock()
			agent.cmdq = append(agent.cmdq, cmd)
			agent.cmdqMux.Unlock()

			// Run the command in another goroutine so we can wait for it
			// (and possibly timeout) in this goroutine.
			cmdDone := make(chan error)
			go func() {
				var err error
				if cmd.Service == "" {
					switch cmd.Cmd {
					// Agent command
					case "SetLogLevel":
						err = agent.handleSetLogLevel(cmd)
					case "StartService":
						err = agent.handleStartService(cmd)
					case "StopService":
						err = agent.handleStopService(cmd)
					default:
						err = pct.UnknownCmdError{Cmd: cmd.Cmd}
					}
				} else {
					// Service command
					manager, ok := agent.services[cmd.Service]
					if ok {
						if manager.IsRunning() {
							err = manager.Handle(cmd)
						} else {
							err = pct.ServiceIsNotRunningError{Service: cmd.Service}
						}
					} else {
						err = pct.UnknownServiceError{Service: cmd.Service}
					}
				}
				cmdDone <- err
			}()

			// Wait for the cmd to complete.
			var err error
			cmdTimeout := time.After(time.Duration(5) * time.Second) // todo
			select {
			case err = <-cmdDone:
			case <-cmdTimeout:
				err = pct.CmdTimeoutError{Cmd: cmd.Cmd}
				// @todo kill that ^ goroutine
			}

			// Reply to the command: just the error if any.  The user can check
			// the log for details about running the command because the cmd
			// should have been associated with the log entries in the cmd handler
			// function by calling LogWriter.Re().
			agent.status.UpdateRe("AgentCmdHandler", "Replying", cmd)
			if err != nil {
				replyChan <- cmd.Reply(err, nil)
			} else {
				replyChan <- cmd.Reply(nil, nil) // success
			}

			// Pop the cmd from the queue.
			agent.status.UpdateRe("AgentCmdHandler", "Finishing", cmd)
			agent.cmdqMux.Lock()
			agent.cmdq = agent.cmdq[0 : len(agent.cmdq)-1]
			agent.cmdqMux.Unlock()

			agent.status.Update("AgentCmdHandler", "Ready")
		} // select
	} // for
}

func (agent *Agent) handleSetLogLevel(cmd *proto.Cmd) error {
	agent.status.UpdateRe("AgentCmdHandler", "SetLogLevel", cmd)
	agent.logger.Info(cmd)

	log := new(proto.LogLevel)
	if err := json.Unmarshal(cmd.Data, log); err != nil {
		agent.logger.Error(err)
		return err
	}

	agent.logRelay.LogLevelChan() <- byte(log.Level)

	return nil
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

	// Return error if service is running.  To keep things simple,
	// we do not restart the service or verifty that the given config
	// matches the running config.  Only stopped services can be started.
	if m.IsRunning() {
		return pct.ServiceIsRunningError{Service: s.Name}
	}

	// Start the service with the given config.
	err := m.Start(cmd, s.Config)
	return err
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
			data := agent.getStatus()
			replyChan <- cmd.Reply(nil, data)
		}
	}
}

func (agent *Agent) getStatus() *proto.StatusData {
	status := agent.status.All()

	for _, m := range agent.services {
		for p, s := range m.Status() {
			status[p] = s
		}
	}

	bytes, err := json.Marshal(status)
	if err != nil {
		// todo
	}
	statusData := &proto.StatusData{}
	if err := json.Unmarshal(bytes, statusData); err != nil {
		// todo
		return nil
	}

	agent.cmdqMux.RLock()
	defer agent.cmdqMux.RUnlock()
	cmds := []string{}
	for _, cmd := range agent.cmdq {
		if cmd != nil {
			cmds = append(cmds, cmd.String())
		}
	}
	statusData.AgentCmdQueue = cmds

	return statusData
}
