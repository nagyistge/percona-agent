package qan

import (
	"encoding/json"
	"fmt"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-tools/pct"
	"sync"
	"time"
)

type Manager struct {
	logger   *pct.Logger
	iter     IntervalIter
	dataChan chan interface{}
	// --
	config              *Config // nil if not running
	configMux           *sync.RWMutex
	workers             map[*Worker]bool
	workersMux          *sync.RWMutex
	workersDoneChan     chan *Worker
	resultChan          chan *Result
	status              *pct.Status
	runWorkersDoneChan  chan bool
	reapWorkersDoneChan chan bool
	sendDataDoneChan    chan bool
}

func NewManager(logger *pct.Logger, iter IntervalIter, dataChan chan interface{}) *Manager {
	m := &Manager{
		logger:   logger,
		iter:     iter,
		dataChan: dataChan,
		// --
		config:              nil, // not running yet
		configMux:           new(sync.RWMutex),
		workers:             make(map[*Worker]bool),
		workersMux:          new(sync.RWMutex),
		status:              pct.NewStatus([]string{"QanManager", "QanLogParser", "QanDataProcessor"}),
		runWorkersDoneChan:  make(chan bool, 1),
		reapWorkersDoneChan: make(chan bool, 1),
		sendDataDoneChan:    make(chan bool, 1),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	m.status.UpdateRe("Qan", "Starting", cmd)
	defer m.status.UpdateRe("Qan", "Ready", cmd)

	logger := m.logger
	logger.InResponseTo(cmd)
	defer logger.InResponseTo(nil)
	logger.Info("Starting")

	m.configMux.Lock()
	defer m.configMux.Unlock()

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

	// Set the slow log apctording to the confnig.
	// @todo

	m.config = c

	// Start runWorkers and sendData goroutines
	m.resultChan = make(chan *Result, c.MaxWorkers)
	m.workersDoneChan = make(chan *Worker, c.MaxWorkers)
	go m.runWorkers(m.config)
	go m.reapWorkers()
	go m.sendData()

	logger.Info("Running")

	return nil
}

func (m *Manager) Stop(cmd *proto.Cmd) error {
	m.status.UpdateRe("Qan", "Stopping", cmd)
	defer m.status.UpdateRe("Qan", "Ready", cmd)

	m.logger.InResponseTo(cmd)
	defer m.logger.InResponseTo(nil)
	m.logger.Info("Stopping")

	m.iter.Stop()            // stop runWorkers
	close(m.workersDoneChan) // stop reapWorkers
	close(m.resultChan)      // stop sendData

	// Wait for the goroutines to terminate.  If this takes too long,
	// let the caller timeout.
	m.status.UpdateRe("Qan", "Stopping, waiting for runWorkers", cmd)
	<-m.runWorkersDoneChan

	m.status.UpdateRe("Qan", "Stopping, waiting for reapWorker", cmd)
	<-m.reapWorkersDoneChan

	m.status.UpdateRe("Qan", "Stopping, waiting for sendData", cmd)
	<-m.sendDataDoneChan

	m.status.UpdateRe("Qan", "Stopping, unsetting config", cmd)
	m.configMux.Lock()
	defer m.configMux.Unlock()
	m.config = nil

	m.logger.Info("Stopped")

	return nil
}

func (m *Manager) Status() string {
	m.configMux.RLock()
	defer m.configMux.RUnlock()

	m.workersMux.RLock()
	defer m.workersMux.RUnlock()

	m.status.Lock()
	defer m.status.Unlock()

	var running bool
	var config []byte
	// Do NOT call IsRunning() because we have locked configMux and
	// IsRunning() locks it too--deadlock!
	if m.config != nil {
		running = true
		config, _ = json.Marshal(m.config)
	}

	status := fmt.Sprintf(`Running: %t
Config: %s
Workers: %d
Manager: %s
RunWorkers: %s
SendData: %s
`,
		running,
		config,
		len(m.workers),
		m.status.Get("Qan", false),
		m.status.Get("QanLogParser", false),
		m.status.Get("sendData", false))

	return status
}

func (m *Manager) IsRunning() bool {
	// Do NOT call this from any function that has locked configMux else we'll deadlock!
	m.configMux.RLock()
	defer m.configMux.RUnlock()

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
// Run workers to process intervals and generate results
/////////////////////////////////////////////////////////////////////////////

// @goroutine
func (m *Manager) runWorkers(config *Config) {
	defer func() { m.runWorkersDoneChan <- true }()

	m.status.Update("QanLogParser", "Waiting for first interval")

	m.iter.Start()
	intervalChan := m.iter.IntervalChan()
	for interval := range intervalChan {
		m.workersMux.RLock()
		runningWorkers := len(m.workers)
		m.workersMux.RUnlock()
		if runningWorkers >= config.MaxWorkers {
			m.logger.Warn("All workers busy, interval dropped")
			continue
		}

		m.status.Update("QanLogParser", "running worker")
		logger := pct.NewLogger(m.logger.LogChan(), "qan-worker")
		job := &Job{
			SlowLogFile:    interval.Filename,
			StartOffset:    interval.StartOffset,
			StopOffset:     interval.StopOffset,
			Runtime:        time.Duration(config.WorkerRuntime) * time.Second,
			ExampleQueries: config.ExampleQueries,
		}
		w := NewWorker(logger, job, m.resultChan, m.workersDoneChan)
		go w.Run()

		m.workersMux.Lock()
		m.workers[w] = true
		m.workersMux.Unlock()

		m.status.Update("QanLogParser", "Ready")
	}
}

// @goroutine
func (m *Manager) reapWorkers() {
	defer func() { m.reapWorkersDoneChan <- true }()
	for worker := range m.workersDoneChan {
		m.status.Update("QanLogParser", "reaping worker")
		m.workersMux.Lock()
		delete(m.workers, worker)
		m.workersMux.Unlock()
	}
}

/////////////////////////////////////////////////////////////////////////////
// Process results and send to API
/////////////////////////////////////////////////////////////////////////////

// @goroutine
func (m *Manager) sendData() {
	defer func() { m.sendDataDoneChan <- true }()
	m.status.Update("QanDataProcessor", "Waiting for first result")
	for result := range m.resultChan {
		m.status.Update("sendData", "Sending result")
		// todo: make {agent:{...}, meta:{...}, data:{...}}
		m.dataChan <- result
		m.status.Update("sendData", "Waiting for result")
	}
}
