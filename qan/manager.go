package qan

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"os"
	"time"
)

type Manager struct {
	logger    *pct.Logger
	mysqlConn mysql.Connector
	iter      IntervalIter
	dataChan  chan interface{}
	// --
	config         *Config // nil if not running
	workers        map[*Worker]bool
	workerDoneChan chan *Worker
	status         *pct.Status
	sync           *pct.SyncChan
	oldSlowLogs    map[string]int
}

func NewManager(logger *pct.Logger, mysqlConn mysql.Connector, iter IntervalIter, dataChan chan interface{}) *Manager {
	m := &Manager{
		logger:    logger,
		mysqlConn: mysqlConn,
		iter:      iter,
		dataChan:  dataChan,
		// --
		workers:     make(map[*Worker]bool),
		status:      pct.NewStatus([]string{"Qan", "QanLogParser"}),
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
	m.status.UpdateRe("Qan", "Starting", cmd)

	logger := m.logger
	logger.InResponseTo(cmd)
	defer logger.InResponseTo(nil)
	logger.Info("Starting")

	if m.config != nil {
		err := pct.ServiceIsRunningError{Service: "qan"}
		logger.Error(err)
		return err
	}

	c := new(Config)
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

	m.workerDoneChan = make(chan *Worker, c.MaxWorkers)
	m.config = c
	go m.run()

	m.status.UpdateRe("Qan", "Running", cmd)
	logger.Info("Running")

	return nil
}

func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.UpdateRe("Qan", "Stopping", cmd)

	m.logger.InResponseTo(cmd)
	defer m.logger.InResponseTo(nil)
	m.logger.Info("Stopping")

	m.status.UpdateRe("Qan", "Stopping", cmd)
	m.sync.Stop()
	m.sync.Wait()

	var err error
	if err = m.mysqlConn.Set(m.config.Stop); err != nil {
		m.logger.Warn(err)
	} else {
		m.logger.Info("Stopped")
	}

	m.config = nil
	m.workerDoneChan = nil
	m.status.UpdateRe("Qan", "Stopped", cmd)

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

func (m *Manager) Do(cmd *proto.Cmd) error {
	return pct.UnknownCmdError{Cmd: cmd.Cmd}
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (m *Manager) run() {
	defer func() {
		if m.sync.IsGraceful() {
			m.status.Update("QanLogParser", "Stopped")
		} else {
			m.status.Update("QanLogParser", "Crashed")
		}
		m.sync.Done()
	}()

	m.status.Update("QanLogParser", "Waiting for first interval")
	m.iter.Start()
	intervalChan := m.iter.IntervalChan()

	for {
		runningWorkers := len(m.workers)
		m.status.Update("QanLogParser", fmt.Sprintf("Ready (%d of %d running)", runningWorkers, m.config.MaxWorkers))

		select {
		case interval := <-intervalChan:
			runningWorkers := len(m.workers)
			if runningWorkers >= m.config.MaxWorkers {
				m.logger.Warn("All workers busy, interval dropped")
				continue
			}

			if interval.StopOffset >= m.config.MaxSlowLogSize {
				if err := m.rotateSlowLog(interval); err != nil {
					m.logger.Error(err)
				}
			}

			m.status.Update("QanLogParser", "Running worker")
			logger := pct.NewLogger(m.logger.LogChan(), "qan-worker")
			job := &Job{
				SlowLogFile:    interval.Filename,
				StartOffset:    interval.StartOffset,
				StopOffset:     interval.StopOffset,
				RunTime:        time.Duration(m.config.WorkerRunTime) * time.Second,
				ExampleQueries: m.config.ExampleQueries,
			}
			w := NewWorker(logger, job, m.dataChan, m.workerDoneChan)
			go w.Run()
			m.workers[w] = true
		case worker := <-m.workerDoneChan:
			m.status.Update("QanLogParser", "Reaping worker")
			delete(m.workers, worker)

			for file, cnt := range m.oldSlowLogs {
				if cnt == 1 {
					m.status.Update("QanLogParser", "Removing old slow log: "+file)
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
	interval.StopOffset, _ = pct.FileSize(newSlowLogFile) // todo: handle err

	// Save old slow log and remove later if configured to do so.
	if m.config.RemoveOldSlowLogs {
		m.oldSlowLogs[newSlowLogFile] = len(m.workers) + 1
	}

	return nil
}
