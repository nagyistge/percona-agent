/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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
	"fmt"
	"strings"

	"github.com/percona/percona-agent/agent"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/mrms"
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

func NewManager(logger *pct.Logger, configDir string, api pct.APIConnector, mrm mrms.Monitor) *Manager {
	repo := NewRepo(pct.NewLogger(logger.LogChan(), "instance-repo"), configDir)
	m := &Manager{
		logger:    logger,
		configDir: configDir,
		api:       api,
		// --
		status:         pct.NewStatus([]string{"instance", "instance-repo", "instance-mrms"}),
		repo:           repo,
		mrm:            mrm,
		mrmChans:       make(map[string]<-chan bool),
		mrmsGlobalChan: make(chan string, 100), // monitor up to 100 instances
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start() error {
	m.status.Update("instance", "Starting")
	if err := m.repo.Init(); err != nil && err != pct.ErrNoSystemTree {
		return err
	}

	m.logger.Info("Started")
	m.status.Update("instance", "Running")

	mrmsGlobalChan, err := m.mrm.GlobalSubscribe()
	if err != nil {
		return err
	}

	for _, instance := range m.GetMySQLInstances() {
		ch, err := m.mrm.Add(instance.DSN)
		if err != nil {
			m.logger.Error("Cannot add instance to the monitor:", err)
			continue
		}
		safeDSN := mysql.HideDSNPassword(instance.DSN)
		m.status.Update("instance", "Getting info "+safeDSN)
		if err := GetMySQLInfo(instance); err != nil {
			m.logger.Warn(fmt.Sprintf("Failed to get MySQL info %s: %s", safeDSN, err))
			continue
		}
		m.status.Update("instance", "Updating info "+safeDSN)
		m.pushInstanceInfo(instance)
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

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// Adds a MySQL instance to MRM
func (m *Manager) addMRMInstance(inst proto.Instance) error {
	ch, err := m.mrm.Add(inst.DSN)
	if err != nil {
		return err
	}
	// We need to store the channel because we need to provide it on the call to the removal of the DSN from MRM
	// This needs to be addressed later on MRM manager.
	m.mrmChans[inst.DSN] = ch

	safeDSN := mysql.HideDSNPassword(inst.DSN)
	m.status.Update("instance", "Getting info "+safeDSN)
	if err := GetMySQLInfo(inst); err != nil {
		m.logger.Warn(fmt.Sprintf("Failed to get MySQL info %s: %s", safeDSN, err))
		return err
	}
	m.status.Update("instance", "Updating info "+safeDSN)
	err = m.pushInstanceInfo(inst)
	if err != nil {
		return err
	}
	return nil
}

// Auxiliary function to add MySQL instances on MRM
func (m *Manager) mrmAddMySQL(added []proto.Instance) {
	// Process added instances
	for _, addIt := range added {
		if err := m.addMRMInstance(addIt); err != nil {
			m.logger.Error(err)
		}
	}
}

// Auxiliary function to remove deleted MySQL instances from MRM
func (m *Manager) mrmDeleteMySQL(deleted []proto.Instance) {
	for _, dltIt := range deleted {
		m.mrm.Remove(dltIt.DSN, m.mrmChans[dltIt.DSN])
		delete(m.mrmChans, dltIt.DSN)
	}
}

// Auxiliary function to update MySQL instances on MRM
func (m *Manager) mrmUpdateMySQL(updated []proto.Instance) {
	// For now updates means deleting and then adding DSN to MRM
	m.mrmDeleteMySQL(updated)
	m.mrmAddMySQL(updated)
}

// Auxiliary function that will take a slice of uuids and return a new slice with its corresponding proto.Instances
func (m *Manager) getInstances(uuids []string) []proto.Instance {
	var insts []proto.Instance
	for _, uuid := range uuids {
		it, err := m.repo.Get(uuid)
		if err != nil {
			// This should never happen, this reflects a bug in the API code that builds the proto.SystemTreeSync
			// document with instances UUIDs in either added, deleted or updated slices that are not present in the
			// tree. Log the error and keep going, we need to fulfill as much operations as possible.
			m.logger.Error(fmt.Sprintf("Could not find UUID '%s' in local registry", uuid))
			continue
		}
		insts = append(insts, it)
	}
	return insts
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("instance", "Handling", cmd)
	defer m.status.Update("instance", "Running")

	switch cmd.Cmd {
	case "UpdateSystemTree":
		var sync *proto.SystemTreeSync
		if err := json.Unmarshal(cmd.Data, &sync); err != nil {
			return cmd.Reply(nil, err)
		}
		err := m.repo.UpdateSystemTree(sync.Tree, sync.Version)
		if err != nil {
			return cmd.Reply(nil, err)
		}
		// For the following block segment we only care about MySQL instances
		// From former code logic, we don't actually care about if there was an error
		// while updating MRM. TODO: really?
		m.mrmAddMySQL(onlyMySQLInsts(m.getInstances(sync.Added)))
		m.mrmDeleteMySQL(onlyMySQLInsts(m.getInstances(sync.Deleted)))
		m.mrmUpdateMySQL(onlyMySQLInsts(m.getInstances(sync.Updated)))
		return cmd.Reply(nil, nil)
	case "GetSystemTree":
		// GetTree will return the tree plus its version in a proto.SystemTreeSync
		tree, err := m.repo.GetSystemTree()
		if err != nil {
			return cmd.Reply(nil, err)
		}
		version := m.repo.GetTreeVersion()
		sync := proto.SystemTreeSync{}
		sync.Tree = tree
		sync.Version = version
		return cmd.Reply(sync, nil)
	case "GetInfo":
		var it *proto.Instance
		if err := json.Unmarshal(cmd.Data, &it); err != nil {
			return cmd.Reply(nil, err)
		}
		err := m.handleGetInfo(*it)
		return cmd.Reply(it, err)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	list := m.repo.List()
	uuids := make([]string, len(list))
	for _, it := range list {
		uuids = append(uuids, it.UUID)
	}
	m.status.Update("instance-repo", strings.Join(uuids, " "))
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) Repo() *Repo {
	return m.repo
}

func (m *Manager) handleGetInfo(it proto.Instance) error {
	if !IsMySQLInstance(it) {
		return fmt.Errorf("Don't know how to get info for %s instance", it.UUID)
	}
	return GetMySQLInfo(it)
}

func GetMySQLInfo(it proto.Instance) error {
	conn := mysql.NewConnection(it.DSN)
	if err := conn.Connect(1); err != nil {
		return err
	}
	defer conn.Close()
	sql := "SELECT /* percona-agent */" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	// Need auxiliary vars because can't get map attribute addresses
	var hostname, distro, version string
	err := conn.DB().QueryRow(sql).Scan(
		&hostname,
		&distro,
		&version,
	)
	if err != nil {
		return err
	}
	it.Properties["hostname"] = hostname
	it.Properties["distro"] = distro
	it.Properties["version"] = version
	return nil
}

// Returns a slice of all MySQL proto.Instances in the system
func (m *Manager) GetMySQLInstances() []proto.Instance {
	m.logger.Debug("GetMySQLInstances:call")
	defer m.logger.Debug("GetMySQLInstances:return")
	return m.Repo().GetMySQLInstances()
}

func (m *Manager) monitorInstancesRestart(ch chan string) {
	m.logger.Debug("monitorInstancesRestart:call")
	defer func() {
		if err := recover(); err != nil {
			m.logger.Error("MySQL connection crashed: ", err)
			m.status.Update("instance-mrms", "Crashed")
		} else {
			m.status.Update("instance-mrms", "Stopped")
		}
		m.logger.Debug("monitorInstancesRestart:return")
	}()

	ch, err := m.mrm.GlobalSubscribe()
	if err != nil {
		m.logger.Error(fmt.Sprintf("Failed to get MySQL restart monitor global channel: %s", err))
		return
	}

	for {
		m.status.Update("instance-mrms", "Idle")
		select {
		case dsn := <-ch:
			safeDSN := mysql.HideDSNPassword(dsn)
			m.logger.Debug("mrms:restart:" + safeDSN)
			m.status.Update("instance-mrms", "Updating "+safeDSN)

			// Get the updated instances list. It should be updated every time since
			// the Add method can add new instances to the list.
			for _, instance := range m.GetMySQLInstances() {
				if instance.DSN != dsn {
					continue
				}
				m.status.Update("instance-mrms", "Getting info "+safeDSN)
				if err := GetMySQLInfo(instance); err != nil {
					m.logger.Warn(fmt.Sprintf("Failed to get MySQL info %s: %s", safeDSN, err))
					break
				}
				m.status.Update("instance-mrms", "Updating info "+safeDSN)
				err := m.pushInstanceInfo(instance)
				if err != nil {
					m.logger.Warn(err)
				}
				break
			}
		}
	}
}

// pushInstanceInfo will update the instance in API with a PUT request
func (m *Manager) pushInstanceInfo(inst proto.Instance) error {
	// Subsystems will be ignored, don't send them
	inst.Subsystems = make([]proto.Instance, 0)
	uri := fmt.Sprintf("%s/%s", m.api.EntryLink("instances"), inst.UUID)
	data, err := json.Marshal(&inst)
	if err != nil {
		m.logger.Error(err)
		return err
	}
	resp, body, err := m.api.Put(m.api.ApiKey(), uri, data)
	if err != nil {
		return err
	}
	// Sometimes the API returns only a status code for an error, without a message
	// so body = nil and in that case string(body) will fail.
	if body == nil {
		body = []byte{}
	}
	if resp != nil && resp.StatusCode != 200 {
		return fmt.Errorf("Failed to PUT: %d, %s", resp.StatusCode, string(body))
	}
	return nil
}
