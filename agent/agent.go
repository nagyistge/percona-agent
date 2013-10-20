package agent

import (
	"os"
	"os/user"
	"time"
	"fmt"
	"sync"
	"reflect"
	"io/ioutil"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/service"
)

const (
	CMD_QUEUE_SIZE = 10
	STATUS_QUEUE_SIZE = 10
	CONFIG_FILE = "/etc/percona/pct-agentd.conf"
)

type Agent struct {
	config *Config
	logRelayer *log.LogRelayer
	cmdClient proto.Client
	statusClient proto.Client
	cc *ControlChannels
	services map[string]service.Manager
	// --
	log *log.LogWriter
	cmdq []*proto.Msg
	cmdqMux *sync.Mutex
	status map[string]string
	statusMux map[string]*sync.Mutex
}

func NewAgent(config *Config, logRelayer *log.LogRelayer, cc *ControlChannels, cmdClient proto.Client, statusClient proto.Client, services map[string]service.Manager) *Agent {
	agent := &Agent{
		config: config,
		logRelayer: logRelayer,
		cc: cc,
		cmdClient: cmdClient,
		statusClient: statusClient,
		services: services,
		// --
		log: log.NewLogWriter(cc.LogChan, "pct-agentd"),
		cmdq: make([]*proto.Msg, CMD_QUEUE_SIZE),
		cmdqMux: new(sync.Mutex),
		status: map[string]string{
			"agent": "",
			"cmd": "",
		},
		statusMux: map[string]*sync.Mutex{
			"agent": new(sync.Mutex),
			"cmd": new(sync.Mutex),
		},
	}
	return agent
}

/*
 * Get data to send to API on connect to authenticate (API key) and let
 * API know how we're currently running.
 */
func (agent *Agent) Hello() map[string] string {
	data := make(map[string]string)

	// API key is in the config, as well as other info like PID file, etc.
	// Map the config struct to our map[string]string.
	cs := reflect.ValueOf(agent.config).Elem()  // config struct
	for i := 0; i < cs.NumField(); i++ {  // foreach field
		data[cs.Type().Field(i).Name] = cs.Field(i).String()
	}

	// Other info the API wants to know:
	u, _ := user.Current()
	data["Hostname"], _ = os.Hostname()
	data["Username"] = u.Username

	return data
}

func (agent *Agent) Run() {
	agent.log.Info("Running agent")

	/*
	 * Start the status and cmd handlers.  Most messages must be serialized because,
	 * for example, handling start-service and stop-service at the same
	 * time would cause weird problems.  The cmdChan serializes messages,
	 * so it's "first come, first serve" (i.e. fifo).  Concurrency has
	 * consequences: e.g. if user1 sends a start-service and it succeeds
	 * and user2 send the same start-service, user2 will get a ServiceIsRunningError.
	 *
	 * Status requests are handled concurrently so the user can always see what
	 * the agent is doing even if it's busy processing commands.
	 */
	agent.statusClient.Run()
	recvStatusChan := agent.statusClient.RecvChan()
	statusChan := make(chan *proto.Msg, STATUS_QUEUE_SIZE)
	go agent.statusHandler(statusChan)

	agent.cmdClient.Run()
	recvCmdChan := agent.cmdClient.RecvChan()
	cmdChan := make(chan *proto.Msg, CMD_QUEUE_SIZE)
	stopChan := make(chan bool, 1)
	doneChan := make(chan bool)
	go agent.cmdHandler(cmdChan, stopChan, doneChan)

	// Reject new msgs after receiving Stop or Update.  To prevent leaving
	// the system in a weird, half-finished state, we stopChan <-true and
	// wait for cmdHandler to finish (<-doneChan).
	stopping := false
	stopReason := ""
	update := false

	// Receive and handle cmd and status requests from the API.
	API_LOOP:
	for {
		if stopping {
			agent.setStatus("agent", "Stopping because " + stopReason, nil)
		} else {
			agent.setStatus("agent", "Ready", nil)
		}
		select {
		case msg := <-recvCmdChan: // from API (wss:/cmd)
			if stopping {
				// Already received Stop or Update, can't do any more work.
				err := CmdRejectedError{Cmd:msg.Cmd, Reason:stopReason}
				sendReplyChan := agent.cmdClient.SendChan()
				sendReplyChan <-msg.Reply(proto.CmdReply{Error: err})
			} else {
				// Try to send the cmd to the cmdHandler.  If the cmdq is not full,
				// this will not block, else the default case will be called and we
				// return a queue full error to let the user know that the agent is
				// too busy.
				agent.setStatus("agent", "Queueing", msg)
				select {
					case cmdChan <-msg:
					default:
						// @todo return quque full error
				}
			}

			// Stop cmdHandler and reject further commands if this one is Stop or Update.
			if msg.Cmd == "Stop" {
				stopChan <-true
				stopping = true
				stopReason = fmt.Sprintf("agent received Stop command [%s]", msg)
			} else if msg.Cmd == "Update" {
				stopChan <-true
				stopping = true
				stopReason = fmt.Sprintf("agent received Update command [%s]", msg)

				// Do self-update after stopping/before exiting.
				update = true
			}
		case msg := <-recvStatusChan: // from API (wss:/status)
			agent.setStatus("agent", "Queueing", msg)
			select {
				case statusChan <-msg:
				default:
					// @todo return quque full error
			}
		case <-agent.cc.StopChan: // from caller (for testing)
			stopChan <-true
			stopping = true
			stopReason = "caller sent true on stop channel"
		case <-doneChan: // from cmdHandler
			close(cmdChan)
			break API_LOOP
		}
	}

	close(statusChan)

	if update {
		agent.selfUpdate()
	}

	agent.setStatus("agent", "Stopped", nil)
	agent.cc.DoneChan <-true
}

func (agent *Agent) cmdHandler(cmdChan chan *proto.Msg, stopChan chan bool, doneChan chan bool) {
	sendReplyChan := agent.cmdClient.SendChan()

	CMD_LOOP:
	for {
		agent.setStatus("cmd", "Ready", nil)
		select {
		case <-stopChan:
			break CMD_LOOP
		case msg := <-cmdChan:
			agent.setStatus("cmd", "Running", msg)
			agent.cmdqMux.Lock()
			agent.cmdq = append(agent.cmdq, msg)
			agent.cmdqMux.Unlock();

			// Run the command in another goroutine so we can wait for it
			// (and possibly timeout) in this goroutine.
			cmdDone := make(chan error)
			go func() {
				var err error
				switch {
				case msg.Cmd == "SetConfig":
					err = agent.handleSetConfig(msg)
				case msg.Cmd == "StartService":
					err = agent.handleStartService(msg)
				case msg.Cmd == "StopService":
					err = agent.handleStopService(msg)
				default:
					err = UnknownCmdError{Cmd:msg.Cmd}
				}
				cmdDone <-err
			}()

			// Wait for the cmd to complete.
			var err error
			cmdTimeout := time.After(time.Duration(msg.Timeout) * time.Second)
			select {
			case err = <-cmdDone:
			case <-cmdTimeout:
				err = CmdTimeoutError{Cmd:msg.Cmd}
			}

			// Reply to the command: just the error if any.  The user can check
			// the log for details about running the command because the msg
			// should have been associated with the log entries in the cmd handler
			// function by calling LogWriter.Re().
			agent.setStatus("cmd", "Replying", msg)
			sendReplyChan <-msg.Reply(proto.CmdReply{Error: err})

			// Pop the msg from the queue.
			agent.setStatus("cmd", "Finishing", msg)
			agent.cmdqMux.Lock()
			agent.cmdq = agent.cmdq[0:len(agent.cmdq) - 1]
			agent.cmdqMux.Unlock()
		}
	}

	// Caller told us to stop, and now we're done.
	agent.setStatus("cmd", "Stopped", nil)
	doneChan <-true
}

/////////////////////////////////////////////////////////////////////////////
// Status Handler
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) statusHandler(statusChan chan *proto.Msg) {
	sendStatusChan := agent.statusClient.SendChan()

	for msg := range statusChan {
		status := new(proto.StatusReply)

		for _, mux := range agent.statusMux {
			mux.Lock();
		}
		agent.cmdqMux.Lock();

		status.Agent = fmt.Sprintf("Agent: %s\nCommand: %s\nStatus: %d\n",
			agent.status["agent"], agent.status["cmd"], len(statusChan));

		status.CmdQueue = make([]string, len(agent.cmdq))
		for _, msg := range agent.cmdq {
			if msg != nil {
				status.CmdQueue = append(status.CmdQueue, msg.String())
			}
		}

		status.Service = make(map[string]string)
		for service, m := range agent.services {
			status.Service[service] = m.Status()
		}

		agent.cmdqMux.Unlock();
		for _, mux := range agent.statusMux {
			mux.Unlock();
		}

		sendStatusChan <-msg.Reply(status)
	}
}

/////////////////////////////////////////////////////////////////////////////
// proto.Msg.Cmd handlers
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) handleSetConfig(msg *proto.Msg) error {
	agent.setStatus("cmd", "SetConfig", msg)

	// Unmarshal the data to get the new config.
	newConfig := new(Config)
	if err := json.Unmarshal(msg.Data, newConfig); err != nil {
		return err
	}

	// Set log file and level if they have changed.
	if agent.config.LogFile != newConfig.LogFile {
		if err := agent.logRelayer.SetLogFile(newConfig.LogFile); err != nil {
			return err
		}
	}
	if agent.config.LogLevel != newConfig.LogLevel {
		if err := agent.logRelayer.SetLogLevel(newConfig.LogLevel); err != nil {
			return err
		}
	}

	// Write PID file if it has changed.  New PID file must not exist.
	if agent.config.PidFile != newConfig.PidFile {
		if err := agent.writePidFile(newConfig.PidFile); err != nil {
			return err
		}
		removeFile(agent.config.PidFile)
	}

	// Always write config file; remove old one if it changed.
	if err := agent.saveConfig(newConfig); err != nil {
		return err
	}
	if agent.config.File != newConfig.File {
		removeFile(agent.config.File)
	}

	// Success: save the new config.
	agent.config = newConfig
	return nil
}

func (agent *Agent) handleStartService(msg *proto.Msg) error {
	agent.setStatus("cmd", "StartService", msg)
	agent.log.Debug("Agent.startService")

	// Unmarshal the data to get the service name and config.
	s := new(proto.ServiceMsg)
	if err := json.Unmarshal(msg.Data, s); err != nil {
		return err
	}

	// Check if we have a manager for the service.
	m, ok := agent.services[s.Name]
	if !ok {
		return UnknownServiceError{Service:s.Name}
	}

	// Return error if service is running.  To keep things simple,
	// we do not restart the service or verifty that the given config
	// matches the running config.  Only stopped services can be started.
	if m.IsRunning() {
		return service.ServiceIsRunningError{Service:s.Name}
	}

	// Start the service with the given config.
	err := m.Start(msg, s.Config)
	return err
}

func (agent *Agent) handleStopService(msg *proto.Msg) error {
	agent.setStatus("cmd", "StopService", msg)
	agent.log.Debug("Agent.stopService")

	// Unmarshal the data to get the service name.
	s := new(proto.ServiceMsg)
	if err := json.Unmarshal(msg.Data, s); err != nil {
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
	err := m.Stop(msg)
	return err
}

/////////////////////////////////////////////////////////////////////////////
// Internal methods
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) setStatus(proc string, status string, msg *proto.Msg) {
	agent.statusMux[proc].Lock()
	if msg != nil {
		status = fmt.Sprintf("%s [%s]", status, msg)
	} else {
		status = fmt.Sprintf("%s", status)
	}
	agent.status[proc] = status
	agent.statusMux[proc].Unlock()
}

func (agent *Agent) stopAllServices() {
}

func (agent *Agent) selfUpdate() {
}

func (agent *Agent) saveConfig(newConfig *Config) error {
	data, err := json.Marshal(newConfig)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(newConfig.File, data, 0766)
}

func (agent *Agent) writePidFile(pidFile string) error {
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

func removeFile (file string) error {
	if file != "" {
		err := os.Remove(file)
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
