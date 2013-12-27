package agent

import (
	// Core
	"encoding/json"
	"fmt"
	golog "log"
	"sync"
	"time"
	// External
	proto "github.com/percona/cloud-protocol"
	// Internal
	pct "github.com/percona/cloud-tools"
	"github.com/percona/cloud-tools/log"
)

const (
	CMD_QUEUE_SIZE    = 10
	STATUS_QUEUE_SIZE = 10
	DEFAULT_LOG_FILE  = "/var/log/pct-agentd.log"
	MAX_ERRORS        = 3
)

type Agent struct {
	auth       *proto.AgentAuth
	logRelayer *log.LogRelayer
	logger     *pct.Logger
	client     proto.WebsocketClient
	services   map[string]pct.ServiceManager
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

func NewAgent(auth *proto.AgentAuth, logRelayer *log.LogRelayer, logger *pct.Logger, client proto.WebsocketClient, services map[string]pct.ServiceManager) *Agent {
	agent := &Agent{
		auth:       auth,
		logRelayer: logRelayer,
		logger:     logger,
		client:     client,
		services:   services,
		// --
		cmdq:       make([]*proto.Cmd, CMD_QUEUE_SIZE),
		cmdqMux:    new(sync.RWMutex),
		status:     pct.NewStatus([]string{"agent", "cmd"}),
		cmdChan:    make(chan *proto.Cmd, CMD_QUEUE_SIZE),
		statusChan: make(chan *proto.Cmd, STATUS_QUEUE_SIZE),
		//
		stopChan: make(chan bool, 1),
	}
	return agent
}

// @goroutine
func (agent *Agent) Run() (stopReason string, update bool) {
	logger := agent.logger
	logger.Info("Running agent")

	// Start client goroutines for sending/receving cmd/reply via channels
	// so we can do non-blocking send/recv.  This only needs to be done once.
	// The chans are buffered, so they work for awhile if not connected.
	client := agent.client
	client.Run()
	cmdChan := client.RecvChan()
	replyChan := client.SendChan()

	// Try and block forever to connect because the agent can't do anything
	// until connected.
	agent.connect()

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

AGENT_LOOP:
	for {
		select {
		case cmd := <-cmdChan: // from API
			if cmd.Cmd == "Abort" {
				golog.Panicf("%s\n", cmd)
			}

			if agent.stopping {
				// Already received Stop or Update, so reject further cmds.
				err := pct.CmdRejectedError{Cmd: cmd.Cmd, Reason: agent.stopReason}
				replyChan <- cmd.Reply(err.Error(), nil)
				continue AGENT_LOOP
			}

			switch cmd.Cmd {
			case "Stop", "Update":
				agent.stopping = true
				if cmd.Cmd == "Stop" {
					agent.stopReason = fmt.Sprintf("agent received Stop command [%s]", cmd)
				} else {
					agent.stopReason = fmt.Sprintf("agent received Update command [%s]", cmd)
					update = true
				}
				go agent.stop(cmd)
			case "Status":
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.statusChan <- cmd: // to statusHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "statusQueue", Size: STATUS_QUEUE_SIZE}
					replyChan <- cmd.Reply(err.Error(), nil)
				}
			default:
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.cmdChan <- cmd: // to cmdHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "cmdQueue", Size: CMD_QUEUE_SIZE}
					replyChan <- cmd.Reply(err.Error(), nil)
				}
			}
		case <-agent.cmdHandlerSync.CrashChan:
			cmdHandlerErrors++
			if cmdHandlerErrors < MAX_ERRORS {
				logger.Error("cmdHandler crashed, restarting")
				go agent.cmdHandler()
			} else {
				logger.Fatal("Too many cmdHandler errors")
			}
		case <-agent.statusHandlerSync.CrashChan:
			statusHandlerErrors++
			if statusHandlerErrors < MAX_ERRORS {
				logger.Error("statusHandler crashed, restarting")
				go agent.statusHandler()
			} else {
				logger.Fatal("Too many statusHandler errors")
			}
		case err := <-client.ErrorChan():
			// websocket closed/crashed/err
			logger.Warn(err)
			if agent.stopping {
				logger.Warn("Lost connection to API while stopping")
			} else {
				agent.connect()
				logger.Info("Reconnected to API")
				cmdHandlerErrors = 0
				statusHandlerErrors = 0
			}
		case <-agent.stopChan:
			break AGENT_LOOP
		} // select

		agent.status.Update("agent", "Ready")

	} // for AGENT_LOOP

	client.Disconnect()

	return agent.stopReason, agent.update
}

// @goroutine
func (agent *Agent) cmdHandler() {
	replyChan := agent.client.SendChan()

	// defer is LIFO, so send done signal last.
	defer agent.cmdHandlerSync.Done()
	defer agent.status.Update("cmd", "Stopped")
	agent.status.Update("cmd", "Ready")

	for {
		select {
		case <-agent.cmdHandlerSync.StopChan: // stop()
			agent.cmdHandlerSync.Graceful()
			return
		case cmd := <-agent.cmdChan:
			agent.status.UpdateRe("cmd", "Running", cmd)

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
							err = manager.Do(cmd)
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
			agent.status.UpdateRe("cmd", "Replying", cmd)
			if err != nil {
				replyChan <- cmd.Reply(err.Error(), nil)
			} else {
				replyChan <- cmd.Reply("", nil)
			}

			// Pop the cmd from the queue.
			agent.status.UpdateRe("cmd", "Finishing", cmd)
			agent.cmdqMux.Lock()
			agent.cmdq = agent.cmdq[0 : len(agent.cmdq)-1]
			agent.cmdqMux.Unlock()

			agent.status.Update("cmd", "Ready")
		} // select
	} // for
}

// @goroutine
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
			status := agent.Status()
			replyChan <- cmd.Reply("", status)
		}
	}
}

func (agent *Agent) Status() *proto.StatusData {
	agent.status.Lock()
	defer agent.status.Unlock()

	agent.cmdqMux.RLock()
	defer agent.cmdqMux.RUnlock()

	status := new(proto.StatusData)

	status.Agent = fmt.Sprintf("Agent: %s\nStopping: %t\nCommand: %s\nStatus: %d\n",
		agent.status.Get("agent", false),
		agent.stopping,
		agent.status.Get("cmd", false),
		len(agent.statusChan))

	status.CmdQueue = make([]string, len(agent.cmdq))
	for _, cmd := range agent.cmdq {
		if cmd != nil {
			status.CmdQueue = append(status.CmdQueue, cmd.String())
		}
	}

	status.Service = make(map[string]string)
	for service, m := range agent.services {
		status.Service[service] = m.Status()
	}

	return status
}

/////////////////////////////////////////////////////////////////////////////
// Command handlers
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) handleSetLogLevel(cmd *proto.Cmd) error {
	agent.status.UpdateRe("cmd", "SetLogLevel", cmd)

	log := new(proto.LogLevel)
	if err := json.Unmarshal(cmd.Data, log); err != nil {
		agent.logger.Error(err)
		return err
	}

	if err := agent.logRelayer.SetLogLevel(log.Level); err != nil {
		agent.logger.Error(err)
		return err
	}

	agent.logger.Info("New log level:" + log.Level)
	return nil
}

func (agent *Agent) handleStartService(cmd *proto.Cmd) error {
	agent.status.UpdateRe("cmd", "StartService", cmd)

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
	agent.status.UpdateRe("cmd", "StopService", cmd)

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

	// If the service is not running, then return.  Stopping a service
	// is an idempotent operation.
	if !m.IsRunning() {
		return nil
	}

	// Stop the service.
	err := m.Stop(cmd)
	return err
}

/////////////////////////////////////////////////////////////////////////////
// Internal methods
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) connect() {
	agent.status.Update("agent", "Connecting to API")
	agent.logger.Info("Connecting to API")
	client := agent.client
	authResponse := new(proto.AuthResponse)
	for {
		client.Disconnect()
		if err := client.Connect(); err != nil {
			agent.logger.Warn(err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := client.Send(agent.auth); err != nil {
			agent.logger.Warn(err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := client.Recv(authResponse); err != nil {
			agent.logger.Warn(err)
			time.Sleep(5 * time.Second)
			continue
		}

		if authResponse.Error != "" {
			agent.logger.Warn(authResponse.Error)
			time.Sleep(5 * time.Second)
			continue
		}

		break // auth success
	}
}

// @goroutine
func (agent *Agent) stop(cmd *proto.Cmd) {
	agent.status.UpdateRe("agent", "stopping cmdHandler", cmd)
	agent.cmdHandlerSync.Stop()
	agent.cmdHandlerSync.Wait()

	for service, manager := range agent.services {
		agent.status.UpdateRe("agent", "stopping"+service, cmd)
		manager.Stop(cmd)
	}

	agent.status.UpdateRe("agent", "stopping statusHandler", cmd)
	agent.statusHandlerSync.Stop()
	agent.statusHandlerSync.Wait()

	agent.status.UpdateRe("agent", "stopping agent", cmd)
	agent.stopChan <- true
}
