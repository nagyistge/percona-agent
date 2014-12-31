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

package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/percona/percona-agent/agent"

	"strconv"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mrms"
	"github.com/percona/percona-agent/mrms/monitor"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
)

type empty struct{}

type Manager struct {
	logger    *pct.Logger
	configDir string
	api       pct.APIConnector
	// --
	status         *pct.Status
	repo           *Repo
	stopChan       chan empty
	mrm            mrms.Monitor
	mrmChans       map[string]<-chan bool
	mrmsGlobalChan chan string
	agentConfig    *agent.Config
}

func NewManager(logger *pct.Logger, configDir string, api pct.APIConnector, mrm mrms.Monitor, conf *agent.Config) *Manager {
	repo := NewRepo(pct.NewLogger(logger.LogChan(), "instance-repo"), configDir, api)
	m := &Manager{
		logger:    logger,
		configDir: configDir,
		api:       api,
		// --
		status:         pct.NewStatus([]string{"instance", "instance-repo"}),
		repo:           repo,
		mrm:            mrm,
		mrmChans:       make(map[string]<-chan bool),
		mrmsGlobalChan: make(chan string, 100), // monitor up to 100 instances
		agentConfig:    conf,
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start() error {
	m.status.Update("instance", "Starting")
	if err := m.repo.Init(); err != nil {
		return err
	}
	m.logger.Info("Started")
	m.status.Update("instance", "Running")

	mrm := m.mrm.(*monitor.Monitor)
	mrmsGlobalChan, err := mrm.GlobalSubscribe()
	if err != nil {
		return err
	}
	instances, err := m.getMySQLInstances()
	if err != nil {
		return err
	}
	for _, instance := range instances {
		ch, err := m.mrm.Add(instance.DSN)
		if err != nil {
			m.logger.Error(fmt.Errorf("Cannot add instance to the monitor: %s", err.Error()))
			continue
		}
		// Store the channel to be able to remove it from mrms
		m.mrmChans[instance.DSN] = ch
	}
	go m.monitorInstancesRestart(mrmsGlobalChan)
	return nil
}

// @goroutine[0]
func (m *Manager) Stop() error {
	// Can't stop the instance manager.
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("instance", "Handling", cmd)
	defer m.status.Update("instance", "Running")

	it := &proto.ServiceInstance{}
	if err := json.Unmarshal(cmd.Data, it); err != nil {
		return cmd.Reply(nil, err)
	}

	switch cmd.Cmd {
	case "Add":
		err := m.repo.Add(it.Service, it.InstanceId, it.Instance, true) // true = write to disk
		if it.Service == "mysql" {
			// Get the instance as type proto.MySQLInstance instead of proto.ServiceInstance
			// because we need the dsn field
			iit := &proto.MySQLInstance{}
			err := m.repo.Get(it.Service, it.InstanceId, iit)
			if err != nil {
				return cmd.Reply(nil, err)
			}
			if err != nil {
				return cmd.Reply(nil, err)
			}
			ch, err := m.mrm.Add(iit.DSN)
			if err != nil {
				m.mrmChans[iit.DSN] = ch
			}
		}
		return cmd.Reply(nil, err)
	case "Remove":
		err := m.repo.Remove(it.Service, it.InstanceId)
		if it.Service == "mysql" {
			// Get the instance as type proto.MySQLInstance instead of proto.ServiceInstance
			// because we need the dsn field
			iit := &proto.MySQLInstance{}
			err := m.repo.Get(it.Service, it.InstanceId, iit)
			if err != nil {
				return cmd.Reply(nil, err)
			}
			if err != nil {
				return cmd.Reply(nil, err)
			}
			m.mrm.Remove(iit.DSN, m.mrmChans[iit.DSN])
		}
		return cmd.Reply(nil, err)
	case "GetInfo":
		info, err := m.handleGetInfo(it.Service, it.Instance)
		return cmd.Reply(info, err)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	m.status.Update("instance-repo", strings.Join(m.repo.List(), " "))
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) Repo() *Repo {
	return m.repo
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) handleGetInfo(service string, data []byte) (interface{}, error) {
	switch service {
	case "mysql":
		it := &proto.MySQLInstance{}
		if err := json.Unmarshal(data, it); err != nil {
			return nil, errors.New("instance.Repo:json.Unmarshal:" + err.Error())
		}
		if it.DSN == "" {
			return nil, fmt.Errorf("MySQL instance DSN is not set")
		}
		if err := GetMySQLInfo(it); err != nil {
			return nil, err
		}
		return it, nil
	default:
		return nil, fmt.Errorf("Don't know how to get info for %s service", service)
	}
}

func GetMySQLInfo(it *proto.MySQLInstance) error {
	conn := mysql.NewConnection(it.DSN)
	if err := conn.Connect(1); err != nil {
		return err
	}
	sql := "SELECT /* percona-agent */" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	err := conn.DB().QueryRow(sql).Scan(
		&it.Hostname,
		&it.Distro,
		&it.Version,
	)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func (m *Manager) getMySQLInstances() ([]*proto.MySQLInstance, error) {
	var instances []*proto.MySQLInstance
	for _, name := range m.Repo().List() {
		parts := strings.Split(name, "-") // mysql-1 or server-12
		if len(parts) != 2 {
			return nil, fmt.Errorf("Invalid instance name: %+v", name)
		}
		if parts[0] == "mysql" {
			id, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, err
			}
			it := &proto.MySQLInstance{}
			err = m.Repo().Get(parts[0], uint(id), it)
			if err != nil {
				return nil, err
			}
			err = GetMySQLInfo(it)
			if err != nil {
				instances = append(instances, it)
			}
		}
	}
	return instances, nil
}

func (m *Manager) monitorInstancesRestart(ch chan string) {
	// Cast mrms monitor as its real type and not the interface
	// because the interface doesn't implements GlobalSubscribe()
	mm := m.mrm.(*monitor.Monitor)
	ch, err := mm.GlobalSubscribe()
	if err != nil {
		m.status.Update("instance", fmt.Sprintf("Error %v", err))
	}

	for dsn := range ch {
		// Get the updated instances list. It should be updated every time since
		// the Add method can add new instances to the list
		instances, err := m.getMySQLInstances()
		if err != nil {
			m.logger.Error(fmt.Errorf("Error in Global Instance Monitor: %v", err.Error()))
		}
		for _, instance := range instances {
			if instance.DSN != dsn {
				continue
			}
			GetMySQLInfo(instance)
			uri := pct.URL(m.agentConfig.ApiHostname, "instances", "mysql", fmt.Sprintf("%d", instance.Id))
			data, err := json.Marshal(instance)
			if err != nil {
				m.logger.Error(err)
				continue
			}
			m.api.Put(m.api.ApiKey(), uri, data)
		}
	}
}
