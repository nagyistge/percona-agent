package qh

import (
	"os"
	"math"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/service"
)

const (
	Name = "qh-manager"
)

type Manager struct {
	cc *agent.ControLChannels
	log *log.LogWriter
	config *Config  // nil if not running
	intervalSyncer *IntervalSyncer
	intervalChan chan *Interval
	resultChan chan *Result
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

func (m *Manager) Start(config []byte) error {
	if m.config != nil {
		return service.ServiceIsRunningError{Service:Name}
	}

	c := new(Config)
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.log.Info("Starting")

	// Set the slow log according to the confnig.
	// @todo

	// Run goroutines to get intervals, run workeres, and send results.
	go m.run(c)

	m.config = c
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
	go getIntervals(config, m.intervalChan, m.intervalSyncer)
	go runWorkers(config, m.intervalChan, resultChan)
	go sendResults(config, m.resultChan)

	select {
	case <-m.cc.stopChan:
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Get synchronized intervals
/////////////////////////////////////////////////////////////////////////////

type Interval struct {
	slowLogFile string
	startTime time.Time
	stopTime time.Time
	startOffset uint64
	stopOffset uint64
}

func getIntervals(config Config, intervalChan chan *Interval, intervalSyncer *IntervalSyncer) error {
	var slowLogFile string
	var slowLogInfo os.FileInfo
	var slowLogFd uintptr
	interval := startInterval()
	ticker := intervalSync.Sync(c.Interval, time.Now().UnixNano(), time.Sleep)
	for t := range tickerChan {
		stopstartInterval(config, interval)
		intervalChan <- interval
	}
	return nil
}

func (m *Manager) startInterval() {
	f := os.NewFile(m.slowLogFd, m.slowLogFile)
	if fileInfo, err := f.Stat(); err != nil {
		return err
	} else {
		m.slowLogInfo = fileInfo
	}
}

func stopInterval() {
}

/////////////////////////////////////////////////////////////////////////////
// Run qh-worker to process intervals
/////////////////////////////////////////////////////////////////////////////

func runWorkers(config Config, intervalChan chan *Interval, resultChan chan *Result) {
	workers := make(map[*Worker]bool)
	doneChan := make(chan *Worker, config.MaxWorkers)

	select:
	case interval := <-intervalChan{
		if len(workers) < 2 {
			job := Job{
				SlowLog: interval.SlowLog,
				StartOffset: interval.StartOffset,
				StopOffset: interval.StopOffset
				--
				Runtime: config.Runtime,
				ExampleQueries: config.ExampleQueries,
				--
				ResultChan: resultChan,
				DoneChan: doneChan,
			}
			w := NewWorker(job)
			workers = append(workers, w)
			w.Run()
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

func sendResults(config Config, resultChan chan *Result) {
	for r := range resultChan {
		// @todo
	}
}

