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

package query

/**
 * query is a proxy manager for service instances. It doesn't have its own config,
 * it's job is to start and stop instances, mostly done in Handle().
 */

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"
)

const (
	SERVICE_NAME = "query"
)

type Manager struct {
	logger  *pct.Logger
	factory InstanceFactory
	im      *instance.Repo
	// --
	instances map[string]Instance
	running   bool
	mux       *sync.RWMutex // guards instances and running
	status    *pct.Status
}

func NewManager(logger *pct.Logger, factory InstanceFactory, im *instance.Repo) *Manager {
	m := &Manager{
		logger:  logger,
		factory: factory,
		im:      im,
		// --
		instances: make(map[string]Instance),
		status:    pct.NewStatus([]string{SERVICE_NAME}),
		mux:       &sync.RWMutex{},
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start() error {
	// todo: should lock here but we call Handle() which also locks
	//m.mux.Lock()
	//defer m.mux.Unlock()

	if m.running {
		return pct.ServiceIsRunningError{Service: SERVICE_NAME}
	}

	// Start all service instances.
	glob := filepath.Join(pct.Basedir.Dir("config"), fmt.Sprintf("%s-*.conf"), SERVICE_NAME)
	configFiles, err := filepath.Glob(glob)
	if err != nil {
		return err
	}

	for _, configFile := range configFiles {
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			m.logger.Error("Read " + configFile + ": " + err.Error())
			continue
		}
		config := &Config{}
		if err := json.Unmarshal(data, config); err != nil {
			m.logger.Error("Decode " + configFile + ": " + err.Error())
			continue
		}
		cmd := &proto.Cmd{
			Ts:   time.Now().UTC(),
			Cmd:  "StartService",
			Data: data,
		}
		reply := m.Handle(cmd)
		if reply.Error != "" {
			m.logger.Error("Start " + configFile + ": " + reply.Error)
			continue
		}
		m.logger.Info("Started " + configFile)
	}

	m.running = true

	m.logger.Info("Started")
	m.status.Update(SERVICE_NAME, "Running")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop() error {
	m.mux.Lock()
	defer m.mux.Unlock()
	for name, instance := range m.instances {
		m.status.Update(SERVICE_NAME, "Stopping "+name)
		if err := instance.Stop(); err != nil {
			m.logger.Warn("Failed to stop " + name + ": " + err.Error())
			continue
		}
		delete(m.instances, name)
	}
	m.running = false
	m.logger.Info("Stopped")
	m.status.Update(SERVICE_NAME, "Stopped")
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe(SERVICE_NAME, "Handling", cmd)
	defer m.status.Update(SERVICE_NAME, "Running")

	switch cmd.Cmd {
	case "StartService":
		config, name, err := m.getInstanceConfig(cmd)
		if err != nil {
			return cmd.Reply(nil, err)
		}

		m.status.UpdateRe(SERVICE_NAME, "Starting "+name, cmd)
		m.logger.Info("Start", name, cmd)

		// Instances names must be unique.
		m.mux.RLock()
		_, haveInstance := m.instances[name]
		m.mux.RUnlock()
		if haveInstance {
			return cmd.Reply(nil, fmt.Errorf("Duplicate instance: %s", err))
		}

		// Create the instance based on its type.
		instance, err := m.factory.Make(config.Service, config.InstanceId, cmd.Data)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("Factory: %s", err))
		}

		// Start the instance.
		if err := instance.Start(); err != nil {
			return cmd.Reply(nil, fmt.Errorf("Start %s: %s", name, err))
		}
		m.mux.Lock()
		m.instances[name] = instance
		m.mux.Unlock()

		// Save the instance-specific config to disk so agent starts on restart.
		instanceConfig := instance.Config()
		if err := pct.Basedir.WriteConfig(name, instanceConfig); err != nil {
			return cmd.Reply(nil, fmt.Errorf("Write %s: %s", name, err))
		}

		return cmd.Reply(nil) // success
	case "StopService":
		_, name, err := m.getInstanceConfig(cmd)
		if err != nil {
			return cmd.Reply(nil, err)
		}
		m.status.UpdateRe(SERVICE_NAME, "Stopping "+name, cmd)
		m.logger.Info("Stop", name, cmd)
		m.mux.RLock()
		instance, ok := m.instances[name]
		m.mux.RUnlock()
		if !ok {
			return cmd.Reply(nil, fmt.Errorf("Unknown instance: %s", name))
		}
		if err := instance.Stop(); err != nil {
			return cmd.Reply(nil, fmt.Errorf("Stop %s: %s", name, err))
		}
		if err := pct.Basedir.RemoveConfig(name); err != nil {
			return cmd.Reply(nil, fmt.Errorf("Remove %s: %s", name, err))
		}
		m.mux.Lock()
		delete(m.instances, name)
		m.mux.Unlock()
		return cmd.Reply(nil) // success
	case "GetConfig":
		config, errs := m.GetConfig()
		return cmd.Reply(config, errs...)
	case "Explain":
		query, name, err := m.getExplainQuery(cmd)
		if err != nil {
			return cmd.Reply(nil, err)
		}
		m.status.UpdateRe(SERVICE_NAME, "Running explain", cmd)
		m.logger.Info("Running explain", name, cmd)

		m.mux.RLock()
		instance, ok := m.instances[name]
		m.mux.RUnlock()
		if !ok {
			return cmd.Reply(nil, fmt.Errorf("Unknown instance: %s", name))
		}

		explain, err := instance.Explain(query)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("Explain %s: %s %#v", name, err, m.instances))
		}
		return cmd.Reply(explain)
	default:
		// SetConfig does not work by design.  To re-configure a instance,
		// stop it then start it again with the new config.
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

// @goroutine[1]
func (m *Manager) Status() map[string]string {
	status := m.status.All()
	m.mux.RLock()
	defer m.mux.RUnlock()
	for _, instance := range m.instances {
		instanceStatus := instance.Status()
		for k, v := range instanceStatus {
			status[k] = v
		}
	}
	return status
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	m.logger.Debug("GetConfig:call")
	defer m.logger.Debug("GetConfig:return")

	m.mux.RLock()
	defer m.mux.RUnlock()

	// Manager does not have its own config. It returns all instances' configs instead.

	// Configs are always returned as array of AgentConfig resources.
	configs := []proto.AgentConfig{}
	errs := []error{}
	for _, instance := range m.instances {
		instanceConfig := instance.Config()
		// Full instance config as JSON string.
		bytes, err := json.Marshal(instanceConfig)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// Just the instance's ServiceInstance, aka ExternalService.
		queryConfig := &Config{}
		if err := json.Unmarshal(bytes, queryConfig); err != nil {
			errs = append(errs, err)
			continue
		}
		config := proto.AgentConfig{
			InternalService: SERVICE_NAME,
			ExternalService: proto.ServiceInstance{
				Service:    queryConfig.Service,
				InstanceId: queryConfig.InstanceId,
			},
			Config:  string(bytes),
			Running: true, // config removed if stopped, so it must be running
		}
		configs = append(configs, config)
	}

	return configs, errs
}

func (m *Manager) getInstanceConfig(cmd *proto.Cmd) (*Config, string, error) {
	/**
	 * cmd.Data is a instance-specific config, e.g. mysql.Config.  But instance-specific
	 * configs embed query.Config, so get that first to determine the instance's name and
	 * type which is all we need to start it.  The instance itself will decode cmd.Data
	 * into it's specific config, which we fetch back later by calling instance.Config()
	 * to save to disk.
	 */
	config := &Config{}
	if cmd.Data != nil {
		if err := json.Unmarshal(cmd.Data, config); err != nil {
			return nil, "", fmt.Errorf("%s.getInstanceConfig:json.Unmarshal:%s", SERVICE_NAME, err)
		}
	}

	// The real name of the internal service, e.g. query-mysql-1:
	name := m.getInstanceName(config.Service, config.InstanceId)

	return config, name, nil
}

func (m *Manager) getInstanceName(service string, instanceId uint) (name string) {
	// The real name of the internal service, e.g. query-mysql-1:
	instanceName := m.im.Name(service, instanceId)
	name = fmt.Sprintf("%s-%s", SERVICE_NAME, instanceName)
	return name
}

func (m *Manager) getExplainQuery(cmd *proto.Cmd) (query string, name string, err error) {
	/**
	 * cmd.Data is a instance-specific config, e.g. mysql.Config.  But instance-specific
	 * configs embed query.Config, so get that first to determine the instance's name and
	 * type which is all we need to start it.  The instance itself will decode cmd.Data
	 * into it's specific config, which we fetch back later by calling instance.Config()
	 * to save to disk.
	 */
	var explainQuery mysql.ExplainQuery
	if cmd.Data != nil {
		if err := json.Unmarshal(cmd.Data, &explainQuery); err != nil {
			return "", "", fmt.Errorf("%s.getInstanceConfig:json.Unmarshal:%s", SERVICE_NAME, err)
		}
	}

	// Get the query
	query = explainQuery.Query

	// The real name of the internal service, e.g. query-mysql-1:
	name = m.getInstanceName(explainQuery.Service, explainQuery.InstanceId)

	return query, name, nil
}
