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

package data

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"os"
	"time"
)

type Manager struct {
	logger   *pct.Logger
	hostname string
	client   pct.WebsocketClient
	// --
	config    *Config
	configDir string
	sz        Serializer
	spooler   Spooler
	sender    *Sender
	status    *pct.Status
}

func NewManager(logger *pct.Logger, hostname string, client pct.WebsocketClient) *Manager {
	m := &Manager{
		logger:   logger,
		hostname: hostname,
		client:   client,
		// --
		status: pct.NewStatus([]string{"data"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if m.config != nil {
		err := pct.ServiceIsRunningError{Service: "data"}
		return err
	}

	// proto.Cmd[Service:agent, Cmd:StartService, Data:proto.ServiceData[Name:data Config:data.Config]]
	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	if err := pct.MakeDir(c.Dir); err != nil {
		return err
	}

	sz, err := makeSerializer(c.Encoding)
	if err != nil {
		return err
	}

	spooler := NewDiskvSpooler(
		pct.NewLogger(m.logger.LogChan(), "data-spooler"),
		c.Dir,
		m.hostname,
	)
	if err := spooler.Start(sz); err != nil {
		return err
	}
	m.spooler = spooler
	m.logger.Info("Started spooler")

	sender := NewSender(
		pct.NewLogger(m.logger.LogChan(), "data-sender"),
		m.client,
	)
	if err := sender.Start(m.spooler, time.Tick(time.Duration(c.SendInterval)*time.Second)); err != nil {
		return err
	}
	m.sender = sender
	m.logger.Info("Started sender")

	m.config = c

	m.status.Update("data", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.sender.Stop()
	m.spooler.Stop()
	m.status.Update("data", "Stopped")
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	defer m.status.Update("data", "Ready")
	switch cmd.Cmd {
	case "GetConfig":
		// proto.Cmd[Service:data, Cmd:GetConfig]
		return cmd.Reply(m.config)
	case "SetConfig":
		newConfig, errs := m.handleSetConfig(cmd)
		return cmd.Reply(newConfig, errs...)
	case "Status":
		// proto.Cmd[Service:data, Cmd:Status]
		status := m.Status()
		return cmd.Reply(status)
	default:
		return cmd.Reply(pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

// @goroutine[0:1]
func (m *Manager) Status() map[string]string {
	return m.status.Merge(m.client.Status(), m.spooler.Status(), m.sender.Status())
}

func (m *Manager) Spooler() Spooler {
	return m.spooler
}

func (m *Manager) Sender() *Sender {
	return m.sender
}

func (m *Manager) LoadConfig(configDir string) ([]byte, error) {
	m.configDir = configDir
	config := &Config{}
	if err := pct.ReadConfig(configDir+"/"+CONFIG_FILE, config); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if config.Dir == "" {
		config.Dir = DEFAULT_DATA_DIR
	}
	if config.SendInterval <= 0 {
		config.SendInterval = DEFAULT_DATA_SEND_INTERVAL
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (m *Manager) WriteConfig(config interface{}, name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	m.logger.Info("Writing", file)
	return pct.WriteConfig(file, config)
}

func (m *Manager) RemoveConfig(name string) error {
	if m.configDir == "" {
		return nil
	}
	file := m.configDir + "/" + CONFIG_FILE
	m.logger.Info("Removing", file)
	return pct.RemoveFile(file)
}

func (m *Manager) handleSetConfig(cmd *proto.Cmd) (interface{}, []error) {
	newConfig := &Config{}
	if err := json.Unmarshal(cmd.Data, newConfig); err != nil {
		return nil, []error{err}
	}

	finalConfig := *m.config // copy current config
	errs := []error{}

	/**
	 * Data sender
	 */

	if newConfig.SendInterval != finalConfig.SendInterval {
		m.sender.Stop()
		if err := m.sender.Start(m.spooler, time.Tick(time.Duration(newConfig.SendInterval)*time.Second)); err != nil {
			errs = append(errs, err)
		} else {
			finalConfig.SendInterval = newConfig.SendInterval
		}
	}

	/**
	 * Data spooler
	 */

	if newConfig.Encoding != finalConfig.Encoding {
		sz, err := makeSerializer(newConfig.Encoding)
		if err != nil {
			errs = append(errs, err)
		} else {
			m.spooler.Stop()
			if err := m.spooler.Start(sz); err != nil {
				errs = append(errs, err)
			} else {
				finalConfig.Encoding = newConfig.Encoding
			}
		}
	}

	// Write the new, updated config.  If this fails, agent will use old config if restarted.
	if err := m.WriteConfig(finalConfig, "data"); err != nil {
		errs = append(errs, errors.New("data.WriteConfig:"+err.Error()))
	}

	m.config = &finalConfig
	return m.config, errs
}

func makeSerializer(encoding string) (Serializer, error) {
	switch encoding {
	case "":
		return NewJsonSerializer(), nil
	case "gzip":
		return NewJsonGzipSerializer(), nil
	default:
		return nil, errors.New("Unknown encoding: " + encoding)
	}
}
