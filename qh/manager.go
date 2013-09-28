package qh

import (
	"os"
	"fmt"
	"time"
	"sync"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/service"
	"github.com/percona/percona-cloud-tools/qh/interval"
)

const (
	Name = "qh-manager"
)

type Manager struct {
	cc *agent.ControlChannels
	iter interval.Iter
	resultChan chan *Result
	dataClient proto.Client
	// --
	log *log.LogWriter
	config *Config  // nil if not running
	configMux *sync.Mutex
	workers map[*Worker]bool
	workersMux *sync.Mutex
	status map[string]string
	statusMux map[string]*sync.Mutex
}

func NewManager(cc *agent.ControlChannels, iter interval.Iter, resultChan chan *Result, dataClient proto.Client) *Manager {
	m := &Manager{
		cc: cc,
		iter: iter,
		resultChan: resultChan,
		dataClient: dataClient,
		// --
		log: log.NewLogWriter(cc.LogChan, "qh-manager"),
		config: nil, // not running yet
		configMux: new(sync.Mutex),
		workers: make(map[*Worker]bool),
		workersMux: new(sync.Mutex),
		status: map[string]string{
			"manager": "",
			"runWorkers": "",
			"sendData": "",
		},
		statusMux: map[string]*sync.Mutex{
			"manager": new(sync.Mutex),
			"runWorkers": new(sync.Mutex),
			"sendData": new(sync.Mutex),
		},
	}
	return m
}

func (m *Manager) Start(msg *proto.Msg, config []byte) error {
	m.setStatus("manager", "starting")
	defer m.setStatus("manager", "waiting for command")

	// Log entries are in response to this msg.
	m.log.Re(msg)
	m.log.Info(msg, "Starting")

	if m.config != nil {
		err := service.ServiceIsRunningError{Service:Name}
		m.log.Error(err)
		return err
	}

	c := new(Config)
	if err := json.Unmarshal(config, c); err != nil {
		m.log.Error(err)
		return err
	}

	// Create the data dir if necessary.
	if err := os.Mkdir(c.DataDir, 0775); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	// Set the slow log according to the confnig.
	// @todo

	// Run goroutines to run workers and send results.
	go m.run()

	m.configMux.Lock()
	m.config = c
	m.configMux.Unlock()

	m.log.Info("Started")

	return nil
}

func (m *Manager) Stop() error {
	m.setStatus("manager", "stopping")
	// @todo
	m.setStatus("manager", "stopped")
	return nil
}

func (m *Manager) Status() string {
	m.configMux.Lock()
	defer m.configMux.Unlock()

	m.workersMux.Lock()
	defer m.workersMux.Unlock()

	for _, mux := range m.statusMux {
		mux.Lock();
		defer mux.Unlock();
	}

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
		running, config, len(m.workers),
		m.status["manager"], m.status["runWorkers"], m.status["sendData"])
	return status
}

func (m *Manager) IsRunning() bool {
	// Do NOT call this from any function that has locked configMux
	// else we'll deadlock!
	m.configMux.Lock()
	var isRunning bool
	if m.config != nil {
		// We're running if we have a config.  Call Status() to get more info.
		isRunning = true
	}
	m.configMux.Unlock()
	return isRunning
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) run() {
	ccW := &agent.ControlChannels{
		LogChan: m.cc.LogChan,
		StopChan: make(chan bool),
	}
	go m.runWorkers(ccW)

	ccR := &agent.ControlChannels{
		LogChan: m.cc.LogChan,
		StopChan: make(chan bool),
	}
	go m.sendResults(ccR)

	select {
	case <-m.cc.StopChan:
		ccW.StopChan <-true
		ccR.StopChan <-true
	}
}

func (m *Manager) setStatus(proc string, status string) {
	m.statusMux[proc].Lock()
	m.status[proc] = status
	m.statusMux[proc].Unlock()
}

/////////////////////////////////////////////////////////////////////////////
// Run qh-worker to process intervals
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) runWorkers(cc *agent.ControlChannels) {
	intervalChan := m.iter.IntervalChan()
	doneChan := make(chan *Worker, m.config.MaxWorkers)
	for {
		m.workersMux.Lock()
		runningWorkers := len(m.workers)
		m.workersMux.Unlock()
		if runningWorkers < m.config.MaxWorkers {
			// Wait for an interval, the stop signal, or a worker to finish (if any are running).
			m.setStatus("runWorkers", "waiting for interval")
			select {
			case interval := <-intervalChan:
				m.setStatus("runWorkers", "running worker")
				cc := &agent.ControlChannels{
					LogChan: cc.LogChan,
					// not used: StopChan
					// not used: DoneChan
				}
				job := &Job{
					SlowLogFile: interval.Filename,
					StartOffset: interval.StartOffset,
					StopOffset: interval.StopOffset,
					Runtime: time.Duration(m.config.WorkerRuntime) * time.Second,
					ExampleQueries: m.config.ExampleQueries,
				}
				w := NewWorker(cc, job, m.resultChan, doneChan)
				go w.Run()

				m.workersMux.Lock()
				m.workers[w] = true
				m.workersMux.Unlock()
			case <-cc.StopChan:
				m.setStatus("runWorkers", "stopping")
				break
			case worker := <-doneChan:
				m.setStatus("runWorkers", "reaping worker")
				m.workersMux.Lock()
				delete(m.workers, worker)
				m.workersMux.Unlock()
			}
		} else {
			// All workers are running.  Wait for one to finish before receiving another interval.
			m.setStatus("runWorkers", "waiting for worker")
			worker := <-doneChan
			m.workersMux.Lock()
			delete(m.workers, worker)
			m.workersMux.Unlock()
		}
	}
}

/////////////////////////////////////////////////////////////////////////////
// Send qh-worker results
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) sendResults(cc *agent.ControlChannels) {
	// @todo spool, send from spool, handle ws errors, etc.
	for {
		m.setStatus("sendData", "waiting for result")
		select {
		case <-cc.StopChan:
			m.setStatus("sendData", "stopping")
			break
		case result := <-m.resultChan:
			m.setStatus("sendData", "sending result")
			_ = m.dataClient.Send(result)
		}
	}
}
