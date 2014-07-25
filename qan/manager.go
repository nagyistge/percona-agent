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
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/data"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mrms"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/ticker"
	"os"
	"sync"
	"time"
)

type Manager struct {
	logger        *pct.Logger
	mysqlFactory  mysql.ConnectionFactory
	clock         ticker.Manager
	iterFactory   IntervalIterFactory
	workerFactory WorkerFactory
	spool         data.Spooler
	im            *instance.Repo
	mrm           mrms.Monitor
	// --
	config          *Config
	running         bool
	mux             *sync.RWMutex // guards config and running
	tickChan        chan time.Time
	restartChan     chan bool
	mysqlConn       mysql.Connector
	lastUptime      int64
	lastUptimeCheck time.Time
	iter            IntervalIter
	workers         map[Worker]*Interval
	workersMux      *sync.RWMutex
	workerDoneChan  chan Worker
	status          *pct.Status
	sync            *pct.SyncChan
	oldSlowLogs     map[string]int
	connectionMux   *sync.RWMutex
	connected       uint
}

func NewManager(logger *pct.Logger, mysqlFactory mysql.ConnectionFactory, clock ticker.Manager, iterFactory IntervalIterFactory, workerFactory WorkerFactory, spool data.Spooler, im *instance.Repo, mrm mrms.Monitor) *Manager {
	m := &Manager{
		logger:        logger,
		mysqlFactory:  mysqlFactory,
		clock:         clock,
		iterFactory:   iterFactory,
		workerFactory: workerFactory,
		spool:         spool,
		im:            im,
		mrm:           mrm,
		// --
		mux:            new(sync.RWMutex),
		tickChan:       make(chan time.Time),
		workers:        make(map[Worker]*Interval),
		workersMux:     new(sync.RWMutex),
		workerDoneChan: make(chan Worker, 2),
		status:         pct.NewStatus([]string{"qan", "qan-log-parser", "qan-last-interval", "qan-next-interval"}),
		sync:           pct.NewSyncChan(),
		oldSlowLogs:    make(map[string]int),
		connectionMux:  new(sync.RWMutex),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start() error {
	m.mux.Lock()
	defer m.mux.Unlock()

	if m.config != nil {
		return pct.ServiceIsRunningError{Service: "qan"}
	}

	// Mangaer ("qan" in status) runs indepdent from qan-log-parser.
	m.status.Update("qan", "Starting")
	defer m.status.Update("qan", "Running")

	// Load qan config from disk.
	config := &Config{}
	if err := pct.Basedir.ReadConfig("qan", config); err != nil {
		if os.IsNotExist(err) {
			m.logger.Info("Not enabled")
			return nil
		}
		m.logger.Error("Read qan config:", err)
		return nil
	}

	// Start run()/qan-log-parser.
	if err := m.start(config); err != nil {
		m.logger.Error("Start qan:", err)
		return nil
	}

	m.config = config
	m.running = true

	m.logger.Info("Started")
	return nil // success
}

func (m *Manager) Stop() error {
	m.mux.Lock()
	defer m.mux.Unlock()
	if !m.running {
		return nil
	}
	m.status.Update("qan", "Stopping")
	if err := m.stop(); err != nil {
		m.logger.Error(err)
	}
	m.running = false
	m.logger.Info("Stopped")
	m.status.Update("qan", "Stopped")
	return nil
}

func (m *Manager) Status() map[string]string {
	m.mux.RLock()
	defer m.mux.RUnlock()
	if m.running {
		m.status.Update("qan-next-interval", fmt.Sprintf("%.1fs", m.clock.ETA(m.tickChan)))
	} else {
		m.status.Update("qan-next-interval", "")
	}

	m.workersMux.RLock()
	defer m.workersMux.RUnlock()
	workerStatus := make(map[string]string)
	for w := range m.workers {
		workerStatus[w.Name()] = w.Status()
	}

	return m.status.Merge(workerStatus)
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("qan", "Handling", cmd)
	defer m.status.Update("qan", "Running")

	switch cmd.Cmd {
	case "StartService":
		m.mux.Lock()
		defer m.mux.Unlock()
		if m.running {
			return cmd.Reply(nil, pct.ServiceIsRunningError{Service: "qan"})
		}

		config := &Config{}
		if err := json.Unmarshal(cmd.Data, config); err != nil {
			return cmd.Reply(nil, err)
		}

		// Start run()/qan-log-parser.
		if err := m.start(config); err != nil {
			return cmd.Reply(nil, err)
		}
		m.running = true

		// Save the config.
		m.config = config
		if err := pct.Basedir.WriteConfig("qan", config); err != nil {
			return cmd.Reply(nil, err)
		}

		return cmd.Reply(nil) // success
	case "StopService":
		m.mux.Lock()
		defer m.mux.Unlock()
		if !m.running {
			return cmd.Reply(nil)
		}
		errs := []error{}
		if err := m.stop(); err != nil {
			errs = append(errs, err)
		}
		m.running = false
		if err := pct.Basedir.RemoveConfig("qan"); err != nil {
			errs = append(errs, err)
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
	if m.config == nil {
		return nil, nil
	}
	bytes, err := json.Marshal(m.config)
	if err != nil {
		return nil, []error{err}
	}
	// Configs are always returned as array of AgentConfig resources.
	config := proto.AgentConfig{
		InternalService: "qan",
		// no external service
		Config:  string(bytes),
		Running: m.running,
	}
	return []proto.AgentConfig{config}, nil
}

func (m *Manager) GetRestartChan() chan bool {
	return m.restartChan
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (m *Manager) run(config Config) {
	defer func() {
		if m.sync.IsGraceful() {
			m.status.Update("qan-log-parser", "Stopped")
		} else {
			m.status.Update("qan-log-parser", "Crashed")
		}
		m.sync.Done()
	}()

	m.status.Update("qan-log-parser", "Starting")

	// If time to next interval is more than 1 minute, then start first
	// interval now.  This means first interval will have partial results.
	t := m.clock.ETA(m.tickChan)
	if t > 60 {
		began := ticker.Began(config.Interval, uint(time.Now().UTC().Unix()))
		m.logger.Info("First interval began at", began)
		m.tickChan <- began
	} else {
		m.logger.Info(fmt.Sprintf("First interval begins in %.1f seconds", t))
	}

	intervalChan := m.iter.IntervalChan()
	lastTs := time.Time{}

	for {
		m.logger.Debug("run:wait")

		m.workersMux.RLock()
		runningWorkers := len(m.workers)
		m.workersMux.RUnlock()
		m.status.Update("qan-log-parser", fmt.Sprintf("Idle (%d of %d running)", runningWorkers, config.MaxWorkers))

		select {
		case interval := <-intervalChan:
			m.logger.Debug(fmt.Sprintf("run:interval:%d", interval.Number))

			m.workersMux.RLock()
			runningWorkers := len(m.workers)
			m.workersMux.RUnlock()
			m.logger.Debug(fmt.Sprintf("%d workers running", runningWorkers))
			if runningWorkers >= config.MaxWorkers {
				m.logger.Warn("All workers busy, interval dropped")
				continue
			}

			if interval.EndOffset >= config.MaxSlowLogSize {
				m.logger.Info("Rotating slow log")
				if err := m.rotateSlowLog(config, interval); err != nil {
					m.logger.Error(err)
				}
			}

			m.status.Update("qan-log-parser", "Running worker")
			job := &Job{
				Id:             fmt.Sprintf("%d", interval.Number),
				SlowLogFile:    interval.Filename,
				StartOffset:    interval.StartOffset,
				EndOffset:      interval.EndOffset,
				RunTime:        time.Duration(config.WorkerRunTime) * time.Second,
				ExampleQueries: config.ExampleQueries,
			}
			w := m.workerFactory.Make(fmt.Sprintf("qan-worker-%d", interval.Number))

			m.workersMux.Lock()
			m.workers[w] = interval
			m.workersMux.Unlock()

			// Run a worker to parse this slice of the slow log.
			go func(interval *Interval) {
				m.logger.Debug(fmt.Sprintf("run:interval:%d:start", interval.Number))
				defer func() {
					m.logger.Debug(fmt.Sprintf("run:interval:%d:done", interval.Number))
					if err := recover(); err != nil {
						// Worker caused panic.  Log it as error because this shouldn't happen.
						m.logger.Error(fmt.Sprintf("Lost interval %s: %s", interval, err))
					}
					m.workerDoneChan <- w
				}()

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

				report := MakeReport(config.ServiceInstance, interval, result, config)
				if err := m.spool.Write("qan", report); err != nil {
					m.logger.Warn("Lost report:", err)
				}
			}(interval)
		case worker := <-m.workerDoneChan:
			m.logger.Debug("run:worker:done")
			m.status.Update("qan-log-parser", "Reaping worker")

			m.workersMux.Lock()
			interval := m.workers[worker]
			delete(m.workers, worker)
			m.workersMux.Unlock()

			if interval.StartTime.After(lastTs) {
				t0 := interval.StartTime.Format("2006-01-02 15:04:05")
				t1 := interval.StopTime.Format("15:04:05 MST")
				m.status.Update("qan-last-interval", fmt.Sprintf("%s to %s", t0, t1))
				lastTs = interval.StartTime
			}

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
		case <-m.restartChan:
			if err := m.mysqlSetup(config); err != nil {
				m.logger.Warn("Failed to setup MySQL after server being restarted: %s", err)
				continue
			}
		case <-m.sync.StopChan:
			m.logger.Debug("run:stop")
			m.sync.Graceful()
			return
		}
	}
}

func (m *Manager) createMysqlConn(service string, instanceId uint) error {
	// Get MySQL instance info from service instance database (SID).
	mysqlIt := &proto.MySQLInstance{}
	if err := m.im.Get(service, instanceId, mysqlIt); err != nil {
		return err
	}

	// Connect to MySQL and set global vars to config/enable slow log.
	m.mysqlConn = m.mysqlFactory.Make(mysqlIt.DSN)

	return nil // success
}

func (m *Manager) mysqlConnect(tries uint) error {
	m.connectionMux.Lock()
	defer m.connectionMux.Unlock()
	if err := m.mysqlConn.Connect(tries); err != nil {
		m.logger.Warn("Unable to connect to MySQL: ", err)
		return err
	}
	m.connected++
	return nil
}

func (m *Manager) mysqlConnClose() {
	m.connectionMux.Lock()
	defer m.connectionMux.Unlock()
	if m.connected == 0 {
		return
	}
	m.connected--
	if m.connected == 0 {
		m.mysqlConn.Close()
	}
}

func (m *Manager) mysqlSetup(config Config) error {
	if err := m.mysqlConnect(2); err != nil {
		return err
	}
	defer m.mysqlConnClose()

	// Set global vars to config/enable slow log.
	if err := m.mysqlConn.Set(config.Start); err != nil {
		m.logger.Error("Unable to configure MySQL: %s", err)
		return err
	}

	return nil // success
}

// @goroutine[1]
func (m *Manager) rotateSlowLog(config Config, interval *Interval) error {
	m.logger.Debug("rotateSlowLog:call")
	defer m.logger.Debug("rotateSlowLog:return")

	m.status.Update("qan-log-parser", "Rotating slow log")

	if err := m.mysqlConnect(2); err != nil {
		return err
	}
	defer m.mysqlConnClose()

	// Stop slow log so we don't move it while MySQL is using it.
	if err := m.mysqlConn.Set(config.Stop); err != nil {
		return err
	}

	// Move current slow log by renaming it.
	newSlowLogFile := fmt.Sprintf("%s-%d", interval.Filename, time.Now().UTC().Unix())
	if err := os.Rename(interval.Filename, newSlowLogFile); err != nil {
		return err
	}

	// Re-enable slow log.
	if err := m.mysqlConn.Set(config.Start); err != nil {
		return err
	}

	// Modify interval so worker parses the rest of the old slow log.
	interval.Filename = newSlowLogFile
	interval.EndOffset, _ = pct.FileSize(newSlowLogFile) // todo: handle err

	// Save old slow log and remove later if configured to do so.
	if config.RemoveOldSlowLogs {
		m.workersMux.RLock()
		m.oldSlowLogs[newSlowLogFile] = len(m.workers) + 1
		m.workersMux.RUnlock()
	}

	return nil
}

func (m *Manager) validateConfig(config *Config) error {
	if config.Start == nil || len(config.Start) == 0 {
		return errors.New("qan.Config.Start array is empty")
	}
	if config.Stop == nil || len(config.Stop) == 0 {
		return errors.New("qan.Config.Stop array is empty")
	}
	if config.MaxWorkers < 0 {
		return errors.New("MaxWorkers must be > 0")
	}
	if config.MaxWorkers > 4 {
		return errors.New("MaxWorkers must be < 4")
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

func (m *Manager) start(config *Config) error {
	/**
	 * XXX Presume caller guards m.config with m.mux.
	 */

	m.logger.Debug("start:call")
	defer m.logger.Debug("start:return")

	// Validate the config.
	if err := m.validateConfig(config); err != nil {
		return err
	}

	// Create mysql connection which is needed for enabling slow log
	// and for slow log rotation
	if err := m.createMysqlConn(config.Service, config.InstanceId); err != nil {
		return err
	}

	// Register restart chan
	restartChan, err := m.mrm.Add(m.mysqlConn.DSN())
	if err != nil {
		return err
	}
	m.restartChan = restartChan

	// Configure mysql to enable slow log monitoring
	m.mysqlSetup(*config)

	// Add a tickChan to the clock so it receives ticks at intervals.
	m.clock.Add(m.tickChan, config.Interval, true)

	// Make an iterator for the slow log file at interval ticks.
	filenameFunc := func() (string, error) {
		if err := m.mysqlConnect(1); err != nil {
			return "", err
		}
		defer m.mysqlConnClose()
		file := m.mysqlConn.GetGlobalVarString("slow_query_log_file")
		return file, nil
	}
	m.iter = m.iterFactory.Make(filenameFunc, m.tickChan)
	m.iter.Start()

	// Start qan-log-parser with a copy of the config because it does not use
	// m.mux when it access the config.  Plus, the config isn't dynamic, so
	// it shouldn't change while running.
	go m.run(*config)

	return nil
}

func (m *Manager) stop() error {
	/**
	 * XXX Presume caller guards m.config with m.mux.
	 */

	m.logger.Debug("stop:call")
	defer m.logger.Debug("stop:return")

	// Stop the interval iter and remove tickChan from the clock.
	m.iter.Stop()
	m.iter = nil
	m.clock.Remove(m.tickChan)
	m.mrm.Remove(m.mysqlConn.DSN(), m.restartChan)

	// Stop run()/qan-log-parser.
	m.sync.Stop()
	m.sync.Wait()

	// Turn off MySQL slow log.
	m.logger.Debug("stop:mysql")
	if err := m.mysqlConnect(2); err != nil {
		return err
	}
	defer m.mysqlConnClose()
	if err := m.mysqlConn.Set(m.config.Stop); err != nil {
		return err
	}

	return nil
}
