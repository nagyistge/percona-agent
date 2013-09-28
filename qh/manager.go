package qh

import (
	"os"
	"time"
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
	intervalIter *interval.Iter
	// --
	config *Config  // nil if not running
	log *log.LogWriter
	intervalChan chan *interval.Interval
	resultChan chan *Result
	dataClient proto.Client
}

func NewManager(cc *agent.ControlChannels, intervalIter *interval.Iter) *Manager {
	m := &Manager{
		cc: cc,
		log: log.NewLogWriter(cc.LogChan, "qh-manager"),
		config: nil, // not running yet
		intervalIter: intervalIter,
		intervalChan: make(chan *interval.Interval, 10),
		resultChan: make(chan *Result, 10),
	}
	return m
}

func (m *Manager) Start(msg *proto.Msg, config []byte) error {
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

	// Run goroutines to get intervals, run workeres, and send results.
	go m.run(*c)

	m.config = c
	m.log.Info("Started")
	return nil
}

func (m *Manager) Stop() error {
	return nil
}

func (m *Manager) Status() string {
	return "ok"
}

func (m *Manager) IsRunning() bool {
	if m.config != nil {
		return true
	}
	return false
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) run(config Config) error {
	ccW := &agent.ControlChannels{
		LogChan: m.cc.LogChan,
		StopChan: make(chan bool),
	}
	go runWorkers(ccW, config, m.intervalChan, m.resultChan)

	ccR := &agent.ControlChannels{
		LogChan: m.cc.LogChan,
		StopChan: make(chan bool),
	}
	go sendResults(ccR, config, m.dataClient)

	select {
	case <-m.cc.StopChan:
		ccW.StopChan <- true
		ccR.StopChan <- true
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Run qh-worker to process intervals
/////////////////////////////////////////////////////////////////////////////

func runWorkers(cc *agent.ControlChannels, config Config, intervalChan chan *interval.Interval, resultChan chan *Result) {
	workers := make(map[*Worker]bool)
	doneChan := make(chan *Worker, config.MaxWorkers)
	for {
		if len(workers) < config.MaxWorkers {
			// Wait for an interval, the stop signal, or a worker to finish (if any are running).
			select {
			case interval := <-intervalChan:
				cc := &agent.ControlChannels{
					LogChan: cc.LogChan,
					// not used: StopChan
					// not used: DoneChan
				}
				job := &Job{
					SlowLogFile: interval.FileName,
					StartOffset: interval.StartOffset,
					StopOffset: interval.StopOffset,
					Runtime: time.Duration(config.Runtime) * time.Second,
					ExampleQueries: config.ExampleQueries,
				}
				w := NewWorker(cc, job, resultChan, doneChan)
				go w.Run()
				workers[w] = true
			case <-cc.StopChan:
				break
			case worker := <-doneChan:
				delete(workers, worker)
			}
		} else {
			// All workers are running.  Wait for one to finish before receiving another interval.
			worker := <-doneChan
			delete(workers, worker)
		}
	}
}

/////////////////////////////////////////////////////////////////////////////
// Send qh-worker results
/////////////////////////////////////////////////////////////////////////////

func sendResults(cc *agent.ControlChannels, config Config, dataClient proto.Client) {
	/*
	for r := range resultChan {
		// @todo
	}
	*/
}
