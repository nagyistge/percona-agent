package qan

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"time"
)

type Manager struct {
	logger   *pct.Logger
	iter     IntervalIter
	dataChan chan interface{}
	// --
	config         *Config // nil if not running
	mysql          *mysql.Connection
	workers        map[*Worker]bool
	workerDoneChan chan *Worker
	status         *pct.Status
	sync           *pct.SyncChan
}

func NewManager(logger *pct.Logger, iter IntervalIter, dataChan chan interface{}) *Manager {
	m := &Manager{
		logger:   logger,
		iter:     iter,
		dataChan: dataChan,
		// --
		workers: make(map[*Worker]bool),
		status:  pct.NewStatus([]string{"Qan", "QanLogParser"}),
		sync:    pct.NewSyncChan(),
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
	mysql := mysql.NewConnection(c.DSN)
	if err := mysql.Connect(); err != nil {
		return err
	}
	if err := mysql.Set(c.Start); err != nil {
		return err
	}
	m.mysql = mysql

	m.config = c

	m.workerDoneChan = make(chan *Worker, c.MaxWorkers)
	go m.run(m.config)

	m.status.UpdateRe("Qan", "Ready", cmd)
	logger.Info("Running")

	return nil
}

func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.UpdateRe("Qan", "Stopping", cmd)
	defer m.status.UpdateRe("Qan", "Ready", cmd)

	m.logger.InResponseTo(cmd)
	defer m.logger.InResponseTo(nil)
	m.logger.Info("Stopping")

	m.status.UpdateRe("Qan", "Stopping", cmd)
	m.sync.Stop()
	m.sync.Wait()

	m.config = nil
	m.status.UpdateRe("Qan", "Stopped", cmd)

	var err error
	if err = m.mysql.Set(m.config.Stop); err != nil {
		m.logger.Warn(err)
	} else {
		m.logger.Info("Stopped")
	}

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
func (m *Manager) run(config *Config) {
	defer m.sync.Done()

	m.status.Update("QanLogParser", "Waiting for first interval")

	m.iter.Start()
	intervalChan := m.iter.IntervalChan()
	for {
		select {
		case interval := <-intervalChan:
			runningWorkers := len(m.workers)
			if runningWorkers >= config.MaxWorkers {
				m.logger.Warn("All workers busy, interval dropped")
				continue
			}

			m.status.Update("QanLogParser", "Running worker")
			logger := pct.NewLogger(m.logger.LogChan(), "qan-worker")
			job := &Job{
				SlowLogFile:    interval.Filename,
				StartOffset:    interval.StartOffset,
				StopOffset:     interval.StopOffset,
				RunTime:        time.Duration(config.WorkerRunTime) * time.Second,
				ExampleQueries: config.ExampleQueries,
			}
			w := NewWorker(logger, job, m.dataChan, m.workerDoneChan)
			go w.Run()
			m.workers[w] = true

			m.status.Update("QanLogParser", "Ready")
		case worker := <-m.workerDoneChan:
			m.status.Update("QanLogParser", "Reaping worker")
			delete(m.workers, worker)
		case <-m.sync.StopChan:
			return
		}
	}
}
