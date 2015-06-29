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

package qan

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/cloud-protocol/proto/v2/qan"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mrms"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/ticker"
)

// An AnalyzerInstnace is an Analyzer ran by a Manager, one per MySQL instance
// as configured.
type AnalyzerInstance struct {
	mysqlConn   mysql.Connector
	restartChan <-chan bool
	tickChan    chan time.Time
	analyzer    Analyzer
}

// A Manager runs AnalyzerInstances, one per MySQL instance as configured.
type Manager struct {
	logger          *pct.Logger
	clock           ticker.Manager
	im              *instance.Repo
	mrm             mrms.Monitor
	mysqlFactory    mysql.ConnectionFactory
	analyzerFactory AnalyzerFactory
	// --
	mux       *sync.RWMutex
	running   bool
	analyzers map[string]AnalyzerInstance
	status    *pct.Status
}

func NewManager(
	logger *pct.Logger,
	clock ticker.Manager,
	im *instance.Repo,
	mrm mrms.Monitor,
	mysqlFactory mysql.ConnectionFactory,
	analyzerFactory AnalyzerFactory,
) *Manager {
	m := &Manager{
		logger:          logger,
		clock:           clock,
		im:              im,
		mrm:             mrm,
		mysqlFactory:    mysqlFactory,
		analyzerFactory: analyzerFactory,
		// --
		mux:       &sync.RWMutex{},
		analyzers: make(map[string]AnalyzerInstance),
		status:    pct.NewStatus([]string{"qan"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.logger.Debug("Start:call")
	defer m.logger.Debug("Start:return")

	m.mux.Lock()
	defer m.mux.Unlock()

	if m.running {
		return pct.ServiceIsRunningError{Service: "qan"}
	}

	// Manager ("qan" in status) runs independent from qan-parser.
	m.status.Update("qan", "Starting")
	defer func() {
		m.running = true
		m.logger.Info("Started")
		m.status.Update("qan", "Running")
	}()

	it, err := pct.Basedir.NewConfigIterator("qan")
	if err != nil {
		m.logger.Error(fmt.Sprintf("Cannot read Query Analytics config files: %v", err))
		return nil
	}

	for it.Next() {
		config := new(qan.Config)
		if err := it.Read(config); err != nil {
			m.logger.Error(fmt.Sprintf("Cannot read Query Analytics config for %s: %v", config.UUID, err))
			continue
		}
		// Start the slow log or perf schema analyzer. If it fails that's ok for
		// the qan manager itself (i.e. don't fail this func) because user can fix
		// or reconfigure this analyzer instance later and have qan manager try
		// again to start it.
		// todo: this fails if agent starts before MySQL is running because MRMS
		//       fails to connect to MySQL in mrms/monitor/instance.NewMysqlInstance();
		//       it should succeed and retry until MySQL is online.
		if err := m.startAnalyzer(*config); err != nil {
			m.logger.Error(fmt.Sprintf("Cannot start Query Analytics for %s: %v. Verify that MySQL is running, "+
				"then try again.", config.UUID, err))
			continue
		}
	}
	return nil // success
}

func (m *Manager) Stop() error {
	m.logger.Debug("Stop:call")
	defer m.logger.Debug("Stop:return")

	m.mux.Lock()
	defer m.mux.Unlock()
	if !m.running {
		return nil
	}

	for uuid := range m.analyzers {
		if err := m.stopAnalyzer(uuid); err != nil {
			m.logger.Error(err)
		}
	}

	m.running = false
	m.logger.Info("Stopped")
	m.status.Update("qan", "Stopped")
	return nil
}

func (m *Manager) Status() map[string]string {
	m.mux.RLock()
	defer m.mux.RUnlock()
	status := m.status.All()
	for _, a := range m.analyzers {
		for k, v := range a.analyzer.Status() {
			status[k] = v
		}
	}
	return status
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("qan", "Handling", cmd)
	defer m.status.Update("qan", "Running")

	switch cmd.Cmd {
	case "StartService":
		m.mux.Lock()
		defer m.mux.Unlock()
		if !m.running {
			return cmd.Reply(nil, pct.ServiceIsNotRunningError{Service: "qan"})
		}
		config := qan.Config{}
		if err := json.Unmarshal(cmd.Data, &config); err != nil {
			return cmd.Reply(nil, err)
		}
		if err := m.startAnalyzer(config); err != nil {
			return cmd.Reply(nil, err)
		}
		// Write instance qan config to disk so agent runs qan on restart.
		if err := pct.Basedir.WriteInstanceConfig("qan", config.UUID, config); err != nil {
			return cmd.Reply(nil, err)
		}
		return cmd.Reply(nil) // success
	case "StopService":
		m.mux.Lock()
		defer m.mux.Unlock()
		if !m.running {
			return cmd.Reply(nil, pct.ServiceIsNotRunningError{Service: "qan"})
		}
		errs := []error{}
		for UUID := range m.analyzers {
			if err := m.stopAnalyzer(UUID); err != nil {
				errs = append(errs, err)
			}
			// Remove qan-<uuid>.conf from disk so agent doesn't run qan on restart.
			if err := pct.Basedir.RemoveInstanceConfig("qan", UUID); err != nil {
				errs = append(errs, err)
			}
		}
		return cmd.Reply(nil, errs...)
	case "GetConfig":
		config, errs := m.GetConfig()
		return cmd.Reply(config, errs...)
	default:
		// SetConfig does not work by design.  To re-configure QAN,
		// stop it then start it again with the new config.
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	m.logger.Debug("GetConfig:call")
	defer m.logger.Debug("GetConfig:return")

	m.mux.RLock()
	defer m.mux.RUnlock()

	// Configs are always returned as array of AgentConfig resources.
	configs := []proto.AgentConfig{}
	for _, a := range m.analyzers {
		bytes, err := json.Marshal(a.analyzer.Config())
		if err != nil {
			m.logger.Warn(err)
			continue
		}
		configs = append(configs, proto.AgentConfig{
			Service: "qan",
			UUID:    a.analyzer.Config().UUID,
			Config:  string(bytes),
			Running: true,
		})
	}
	return configs, nil
}

func ValidateConfig(config *qan.Config) error {
	if config.CollectFrom == "" {
		// Before perf schema, CollectFrom didn't exist, so existing default QAN configs
		// don't have it.  To be backwards-compatible, no CollectFrom == slowlog.
		config.CollectFrom = "slowlog"
	}
	if config.CollectFrom != "slowlog" && config.CollectFrom != "perfschema" {
		return fmt.Errorf("Invalid CollectFrom: '%s'.  Expected 'perfschema' or 'slowlog'.", config.CollectFrom)
	}
	if config.Start == nil || len(config.Start) == 0 {
		return errors.New("qan.Config.Start array is empty")
	}
	if config.Stop == nil || len(config.Stop) == 0 {
		return errors.New("qan.Config.Stop array is empty")
	}
	if config.Interval == 0 {
		return errors.New("Interval must be > 0")
	}
	if config.Interval > 3600 {
		return errors.New("Interval must be <= 3600 (1 hour)")
	}
	if config.WorkerRunTime == 0 {
		return errors.New("WorkerRuntime must be > 0")
	}
	if config.WorkerRunTime > 1200 {
		return errors.New("WorkerRuntime must be <= 1200 (20 minutes)")
	}
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) startAnalyzer(config qan.Config) error {
	/*
		XXX Assume caller has locked m.mux.
	*/
	m.logger.Debug("startAnalyzer:call")
	defer m.logger.Debug("startAnalyzer:return")

	// Validate the config. This func may modify the config.
	if err := ValidateConfig(&config); err != nil {
		return fmt.Errorf("Invalid qan.Config: %s", err)
	}

	// Get the MySQL instance from repo.
	mysqlInstance, err := m.im.Get(config.UUID)
	if err != nil {
		return fmt.Errorf("Cannot get MySQL instance from repo: %s", err)
	}

	// Check if an analyzer for this MySQL instance already exists.
	if a, ok := m.analyzers[config.UUID]; ok {
		return pct.ServiceIsRunningError{Service: a.analyzer.String()}

	}
	// Create a MySQL connection.
	mysqlConn := m.mysqlFactory.Make(mysqlInstance.DSN)

	// Add the MySQL DSN to the MySQL restart monitor. If MySQL restarts,
	// the analyzer will stop its worker and re-configure MySQL.
	restartChan, err := m.mrm.Add(mysqlConn.DSN())
	if err != nil {
		return fmt.Errorf("Cannot add MySQL instance to restart monitor: %s", err)
	}

	// Make a chan on which the clock will tick at even intervals:
	// clock -> tickChan -> iter -> analyzer -> worker
	tickChan := make(chan time.Time, 1)
	m.clock.Add(tickChan, config.Interval, true)

	// Create and start a new analyzer. This should return immediately.
	// The analyzer will configure MySQL, start its iter, then run it worker
	// for each interval.
	analyzer := m.analyzerFactory.Make(
		config,
		"qan-analyzer-"+mysqlInstance.Name,
		mysqlConn,
		restartChan,
		tickChan,
	)
	if err := analyzer.Start(); err != nil {
		return fmt.Errorf("Cannot start analyzer: %s", err)
	}

	// Save the new analyzer and its associated parts.
	m.analyzers[config.UUID] = AnalyzerInstance{
		mysqlConn:   mysqlConn,
		restartChan: restartChan,
		tickChan:    tickChan,
		analyzer:    analyzer,
	}

	return nil // success
}

func (m *Manager) stopAnalyzer(uuid string) error {
	/*
		XXX Assume caller has locked m.mux.
	*/

	m.logger.Debug("stopAnalyzer:call")
	defer m.logger.Debug("stopAnalyzer:return")

	a, ok := m.analyzers[uuid]
	if !ok {
		m.logger.Debug("stopAnalyzer:na", uuid)
		return nil
	}

	m.status.Update("qan", fmt.Sprintf("Stopping %s", a.analyzer))
	m.logger.Info(fmt.Sprintf("Stopping %s", a.analyzer))

	// Stop ticking on this tickChan. Other services receiving ticks at the same
	// interval are not affected.
	m.clock.Remove(a.tickChan)

	// Stop watching this MySQL instance. Other services watching this MySQL
	// instance are not affected.
	m.mrm.Remove(a.mysqlConn.DSN(), a.restartChan)

	// Stop the analyzer. It stops its iter and worker and un-configures MySQL.
	if err := a.analyzer.Stop(); err != nil {
		return err
	}

	// Stop managing this analyzer.
	delete(m.analyzers, uuid)
	return nil // success
}
