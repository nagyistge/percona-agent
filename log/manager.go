package log

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"time"
)

type Manager struct {
	client pct.WebsocketClient
	// --
	config    *Config
	configDir string
	logger    *pct.Logger
	relay     *Relay
	status    *pct.Status
}

func NewManager(client pct.WebsocketClient) *Manager {
	m := &Manager{
		client: client,
		// --
		status: pct.NewStatus([]string{"log"}),
	}
	return m
}

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if m.relay != nil {
		err := pct.ServiceIsRunningError{Service: "log"}
		return err
	}

	// proto.Cmd[Service:agent, Cmd:StartService, Data:proto.ServiceData[Name:log Config:log.Config]]
	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	level, ok := proto.LogLevelNumber[c.Level]
	if !ok {
		return errors.New("Invalid log level: " + c.Level)
	}

	if err := pct.MakeDir(c.File); err != nil {
		return err
	}

	m.relay = NewRelay(m.client, c.File, level, c.Offline)
	go m.relay.Run()
	m.config = c

	m.logger = pct.NewLogger(m.relay.LogChan(), "log")

	m.status.Update("log", "Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	// Can't stop the logger yet.
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	defer m.status.Update("log", "Ready")

	switch cmd.Cmd {
	case "SetConfig":
		// proto.Cmd[Service:log, Cmd:SetConfig, Data:log.Config]
		c := &Config{}
		if err := json.Unmarshal(cmd.Data, c); err != nil {
			return cmd.Reply(nil, err)
		}
		errs := []error{}
		if m.config.File != c.File {
			select {
			case m.relay.LogFileChan() <- c.File:
				m.config.File = c.File
			case <-time.After(3 * time.Second):
				errs = append(errs, errors.New("Timeout setting new log file"))
			}
		}
		if m.config.Level != c.Level {
			level, ok := proto.LogLevelNumber[c.Level]
			if !ok {
				return cmd.Reply(nil, errors.New("Invalid log level: "+c.Level))
			}
			select {
			case m.relay.LogLevelChan() <- level:
				m.config.Level = c.Level
			case <-time.After(3 * time.Second):
				errs = append(errs, errors.New("Timeout setting new log level"))
			}
		}
		return cmd.Reply(m.config, errs...)
	case "GetConfig":
		// proto.Cmd[Service:log, Cmd:GetConfig]
		return cmd.Reply(m.config)
	case "Status":
		// proto.Cmd[Service:log, Cmd:Status]
		status := m.InternalStatus()
		return cmd.Reply(status)
	default:
		return cmd.Reply(pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

// @goroutine[0:1]
func (m *Manager) Status() string {
	return m.status.Get("log", true)
}

// @goroutine[0]
func (m *Manager) InternalStatus() map[string]string {
	s := make(map[string]string)
	s["log"] = m.Status()
	return s
}

// @goroutine[0]
func (m *Manager) Relay() *Relay {
	return m.relay
}

func (m *Manager) LoadConfig(configDir string) (interface{}, error) {
	m.configDir = configDir
	v, err := pct.ReadConfig(configDir + "/" + CONFIG_FILE)
	if err != nil {
		return nil, err
	}
	config := v.(Config)
	if config.Level == "" {
		config.Level = DEFAULT_LOG_LEVEL
	} else {
		if _, ok := proto.LogLevelNumber[config.Level]; !ok {
			return nil, errors.New("Invalid log level: " + config.Level)
		}
	}
	return config, nil
}

func (m *Manager) WriteConfig(config interface{}, name string) error {
	// Write a monitor config.
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	if m.logger != nil {
		m.logger.Info("Writing", file)
	}
	return pct.WriteConfig(file, config)
}

func (m *Manager) RemoveConfig(name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	if m.logger != nil {
		m.logger.Info("Removing", file)
	}
	return pct.RemoveFile(file)
}
