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

package qan

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/ticker"
	"os"
	"time"
)

type Manager struct {
	logger        *pct.Logger
	mysqlConn     mysql.Connector
	clock         ticker.Manager
	iterFactory   IntervalIterFactory
	workerFactory WorkerFactory
	spool         data.Spooler
	// --
	config         *Config // nil if not running
	configDir      string
	tickChan       chan time.Time
	iter           IntervalIter
	workers        map[Worker]bool
	workerDoneChan chan Worker
	status         *pct.Status
	sync           *pct.SyncChan
	oldSlowLogs    map[string]int
}

func NewManager(logger *pct.Logger, mysqlConn mysql.Connector, clock ticker.Manager, iterFactory IntervalIterFactory, workerFactory WorkerFactory, spool data.Spooler) *Manager {
	m := &Manager{
		logger:        logger,
		mysqlConn:     mysqlConn,
		clock:         clock,
		iterFactory:   iterFactory,
		workerFactory: workerFactory,
		spool:         spool,
		// --
		workers:     make(map[Worker]bool),
		status:      pct.NewStatus([]string{"qan", "qan-log-parser"}),
		sync:        pct.NewSyncChan(),
		oldSlowLogs: make(map[string]int),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	m.status.UpdateRe("qan", "Starting", cmd)

	logger := m.logger
	logger.InResponseTo(cmd)
	defer logger.InResponseTo(nil)
	logger.Info("Starting")

	// Can't start if already running.
	if m.config != nil {
		err := pct.ServiceIsRunningError{Service: "qan"}
		logger.Error(err)
		return err
	}

	// Parse JSON into Config struct.
	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		logger.Error(err)
		return err
	}

	// Connect to MySQL and set global vars to config/enable slow log.
	if err := m.mysqlConn.Connect(c.DSN); err != nil {
		return err
	}
	if err := m.mysqlConn.Set(c.Start); err != nil {
		return err
	}

	// Add a tickChan to the clock so it receives ticks at intervals.
	m.tickChan = make(chan time.Time)
	m.clock.Add(m.tickChan, c.Interval, true)

	// Make an iterator for the slow log file at interval ticks.
	filenameFunc := func() (string, error) {
		file := m.mysqlConn.GetGlobalVarString("slow_query_log_file")
		return file, nil
	}
	m.iter = m.iterFactory.Make(filenameFunc, m.tickChan)
	m.iter.Start()

	// When Worker finishes parsing an interval, it singals done on this chan.
	m.workerDoneChan = make(chan Worker, c.MaxWorkers)

	// Run Query Analytics!
	go m.run()

	// Save the config.
	m.config = c
	m.status.UpdateRe("qan", "Running", cmd)
	logger.Info("Running")

	if err := m.WriteConfig(c, ""); err != nil {
		return err
	}

	return nil // success
}

func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.UpdateRe("qan", "Stopping", cmd)

	m.logger.InResponseTo(cmd)
	defer m.logger.InResponseTo(nil)
	m.logger.Info("Stopping")

	m.sync.Stop()
	m.sync.Wait()

	var err error
	if err = m.mysqlConn.Set(m.config.Stop); err != nil {
		m.logger.Warn(err)
	} else {
		m.logger.Info("Stopped")
	}

	m.clock.Remove(m.tickChan)
	m.tickChan = nil
	m.workerDoneChan = nil
	m.config = nil

	m.status.UpdateRe("qan", "Stopped", cmd)

	return err
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) IsRunning() bool {
	// We're running if we have a config.
	if m.config != nil {
		return true
	}
	return false // not running
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (m *Manager) run() {
	defer func() {
		if m.sync.IsGraceful() {
			m.status.Update("qan-log-parser", "Stopped")
		} else {
			m.status.Update("qan-log-parser", "Crashed")
		}
		m.sync.Done()
	}()

	m.status.Update("qan-log-parser", "Waiting for first interval")
	intervalChan := m.iter.IntervalChan()

	for {
		runningWorkers := len(m.workers)
		m.status.Update("qan-log-parser", fmt.Sprintf("Ready (%d of %d running)", runningWorkers, m.config.MaxWorkers))

		select {
		case interval := <-intervalChan:
			runningWorkers := len(m.workers)
			if runningWorkers >= m.config.MaxWorkers {
				m.logger.Warn("All workers busy, interval dropped")
				continue
			}

			if interval.EndOffset >= m.config.MaxSlowLogSize {
				if err := m.rotateSlowLog(interval); err != nil {
					m.logger.Error(err)
				}
			}

			m.status.Update("qan-log-parser", "Running worker")
			job := &Job{
				SlowLogFile:    interval.Filename,
				StartOffset:    interval.StartOffset,
				EndOffset:      interval.EndOffset,
				RunTime:        time.Duration(m.config.WorkerRunTime) * time.Second,
				ExampleQueries: m.config.ExampleQueries,
			}
			w := m.workerFactory.Make()
			m.workers[w] = true
			go func() {
				defer func() { m.workerDoneChan <- w }()
				t0 := time.Now()
				result, err := w.Run(job)
				t1 := time.Now()
				if err != nil {
					m.logger.Error(err)
					return
				}
				if result == nil {
					m.logger.Error("Nil result", fmt.Sprintf("+%v", job))
					return
				}
				result.RunTime = t1.Sub(t0).Seconds()
				m.spool.Write("qan", MakeReport(interval, result, m.config))
			}()
		case worker := <-m.workerDoneChan:
			m.status.Update("qan-log-parser", "Reaping worker")
			delete(m.workers, worker)

			for file, cnt := range m.oldSlowLogs {
				if cnt == 1 {
					m.status.Update("qan-log-parser", "Removing old slow log "+file)
					if err := os.Remove(file); err != nil {
						m.logger.Warn(err)
					} else {
						delete(m.oldSlowLogs, file)
						m.logger.Info("Removed " + file)
					}
				} else {
					m.oldSlowLogs[file] = cnt - 1
				}
			}
		case <-m.sync.StopChan:
			m.sync.Graceful()
			return
		}
	}
}

// @goroutine[1]
func (m *Manager) rotateSlowLog(interval *Interval) error {
	// Stop slow log so we don't move it while MySQL is using it.
	if err := m.mysqlConn.Set(m.config.Stop); err != nil {
		return err
	}

	// Move current slow log by renaming it.
	newSlowLogFile := fmt.Sprintf("%s-%d", interval.Filename, time.Now().UTC().Unix())
	if err := os.Rename(interval.Filename, newSlowLogFile); err != nil {
		return err
	}

	// Re-enable slow log.
	if err := m.mysqlConn.Set(m.config.Start); err != nil {
		return err
	}

	// Modify interval so worker parses the rest of the old slow log.
	interval.Filename = newSlowLogFile
	interval.EndOffset, _ = pct.FileSize(newSlowLogFile) // todo: handle err

	// Save old slow log and remove later if configured to do so.
	if m.config.RemoveOldSlowLogs {
		m.oldSlowLogs[newSlowLogFile] = len(m.workers) + 1
	}

	return nil
}

func (m *Manager) LoadConfig(configDir string) ([]byte, error) {
	m.configDir = configDir
	config := &Config{}
	if err := pct.ReadConfig(configDir+"/"+CONFIG_FILE, config); err != nil {
		return nil, err
	}
	// There are no defaults; the config file should have everything we need.
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
