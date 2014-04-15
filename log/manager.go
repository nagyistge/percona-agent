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

package log

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"os"
	"time"
)

type Manager struct {
	client  pct.WebsocketClient
	logChan chan *proto.LogEntry
	// --
	config    *Config
	configDir string
	logger    *pct.Logger
	relay     *Relay
	status    *pct.Status
}

func NewManager(client pct.WebsocketClient, logChan chan *proto.LogEntry) *Manager {
	m := &Manager{
		client:  client,
		logChan: logChan,
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

	m.status.Update("log", "Starting")

	m.relay = NewRelay(m.client, m.logChan, c.File, level, c.Offline)
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
	m.status.UpdateRe("log", "Handling", cmd)
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

		// Write the new, updated config.  If this fails, agent will use old config if restarted.
		if err := pct.Basedir.WriteConfig("log", m.config); err != nil {
			errs = append(errs, errors.New("log.WriteConfig:"+err.Error()))
		}

		return cmd.Reply(m.config, errs...)
	case "GetConfig":
		// proto.Cmd[Service:log, Cmd:GetConfig]
		return cmd.Reply(m.config)
	case "Status":
		// proto.Cmd[Service:log, Cmd:Status]
		status := m.Status()
		return cmd.Reply(status)
	case "Reconnect":
		err := m.client.Disconnect()
		return cmd.Reply(nil, err)
	default:
		return cmd.Reply(pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

// @goroutine[0]
func (m *Manager) Status() map[string]string {
	return m.status.Merge(m.client.Status(), m.relay.Status())
}

// @goroutine[0]
func (m *Manager) Relay() *Relay {
	return m.relay
}

func (m *Manager) LoadConfig() ([]byte, error) {
	config := &Config{}
	if err := pct.Basedir.ReadConfig("log", config); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if config.Level == "" {
		config.Level = DEFAULT_LOG_LEVEL
	} else {
		if _, ok := proto.LogLevelNumber[config.Level]; !ok {
			return nil, errors.New("Invalid log level: " + config.Level)
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return data, nil
}
