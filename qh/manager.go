package qh

import (
//	"os"
	"time"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/service"
)

const (
	Name = "qh-manager"
)

type Manager struct {
	cc *agent.ControlChannels
	log *log.LogWriter
	config *Config  // nil if not running
	intervalSyncer *IntervalSyncer
	intervalChan chan *Interval
	resultChan chan *Result
	dataClient proto.Client
}

func NewManager(cc *agent.ControlChannels, intervalSyncer *IntervalSyncer) *Manager {
	m := &Manager{
		cc: cc,
		log: log.NewLogWriter(cc.LogChan, "qh-manager"),
		config: nil, // not running yet
		intervalSyncer: intervalSyncer,
		intervalChan: make(chan *Interval, 10),
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

func (m *Manager) UpdateConfig(config []byte) error {
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
	ccI := &agent.ControlChannels{
		LogChan: m.cc.LogChan,
		StopChan: make(chan bool),
	}
	go getIntervals(ccI, config, m.intervalChan, m.intervalSyncer)

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
		ccI.StopChan <- true
		ccW.StopChan <- true
		ccR.StopChan <- true
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Get synchronized intervals
/////////////////////////////////////////////////////////////////////////////

type Interval struct {
	SlowLogFile string
	StartTime time.Time
	StopTime time.Time
	StartOffset uint64
	StopOffset uint64
}

func getIntervals(cc *agent.ControlChannels, config Config, intervalChan chan *Interval, intervalSyncer *IntervalSyncer) error {
	/*
	var slowLogFile string
	var slowLogInfo os.FileInfo
	var slowLogFd uintptr
	ticker := intervalSyncer.Sync(float64(time.Now().UnixNano()))
	for t := range ticker.C {
		//stopstartInterval(config, interval)
		//intervalChan <- interval
	}
	*/
	return nil
}

func (m *Manager) startInterval() error {
	/*
	f := os.NewFile(m.slowLogFd, m.slowLogFile)
	if fileInfo, err := f.Stat(); err != nil {
		return err
	} else {
		m.slowLogInfo = fileInfo
	}
	*/
	return nil
}

func stopInterval() {
}

/////////////////////////////////////////////////////////////////////////////
// Run qh-worker to process intervals
/////////////////////////////////////////////////////////////////////////////

func runWorkers(cc *agent.ControlChannels, config Config, intervalChan chan *Interval, resultChan chan *Result) {
	workers := make(map[*Worker]bool)
	doneChan := make(chan *Worker, config.MaxWorkers)

	select {
	case interval := <-intervalChan:
		if len(workers) < 2 {
			cc := &agent.ControlChannels{
				LogChan: cc.LogChan,
				StopChan: make(chan bool),
			}
			job := &Job{
				SlowLogFile: interval.SlowLogFile,
				StartOffset: interval.StartOffset,
				StopOffset: interval.StopOffset,
				Runtime: time.Duration(config.Runtime) * time.Second,
				ExampleQueries: config.ExampleQueries,
				Cc: cc,
				ResultChan: resultChan,
				DoneChan: doneChan,
			}
			w := NewWorker(job)
			w.Run()
			workers[w] = true
		} else {
			// Too many workers running
		}
	case worker := <-doneChan:
		delete(workers, worker)
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

