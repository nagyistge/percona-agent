package mm

import (
	"encoding/json"
	"errors"
	"fmt"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-tools/pct"
)

type Binding struct {
	ticker         pct.Ticker
	collectionChan chan *Collection
	aggregator     *Aggregator
}

// todo: remember originating cmd for start service and start monitor,
//       return with ServiceIsRunningError

type Manager struct {
	logger        *pct.Logger
	monitors      map[string]Monitor
	tickerFactory pct.TickerFactory
	dataChan      chan interface{}
	// --
	config         *Config // nil if not running
	status         *pct.Status
	aggregators    map[uint]*Binding
	collectTickers map[string]pct.Ticker
}

func NewManager(logger *pct.Logger, monitors map[string]Monitor, tickerFactory pct.TickerFactory, dataChan chan interface{}) *Manager {
	m := &Manager{
		logger:        logger,
		monitors:      monitors,
		tickerFactory: tickerFactory,
		dataChan:      dataChan,
		// --
		config:         nil, // not running yet
		status:         pct.NewStatus([]string{"mm"}),
		aggregators:    make(map[uint]*Binding),
		collectTickers: make(map[string]pct.Ticker),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if m.IsRunning() {
		return pct.ServiceIsRunningError{"mm"}
	}

	c := new(Config)
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.status.UpdateRe("mm", "Starting", cmd)

	// We need one aggregator for each unique report interval.  There's usually
	// just one: 60s.  Remember: report interval != collect interval.  Monitors
	// can collect at different intervals (typically 1s and 10s), yet all report
	// at the same 60s interval, or different report intervals.
	for monitorName, interval := range c.Intervals {
		_, haveMonitor := m.monitors[monitorName]
		if !haveMonitor {
			return errors.New("Unknown monitor: " + monitorName)
		}

		if _, ok := m.aggregators[interval.Report]; !ok {
			ticker := m.tickerFactory.Make(interval.Report)
			collectionChan := make(chan *Collection, 2*len(c.Intervals))
			aggregator := NewAggregator(ticker, collectionChan, m.dataChan)

			msg := fmt.Sprintf("Synchronizing %d second report interval", interval.Report)
			m.status.UpdateRe("mm", msg, cmd)
			aggregator.Start()

			m.aggregators[interval.Report] = &Binding{ticker, collectionChan, aggregator}
		}
	}

	m.config = c
	m.status.UpdateRe("mm", "Ready", cmd)
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	// Stop all monitors.
	for name, monitor := range m.monitors {
		m.status.UpdateRe("mm", "Stopping "+name, cmd)
		monitor.Stop()
	}

	// Stop and remove all aggregators.
	for n, b := range m.aggregators {
		b.aggregator.Stop()
		b.ticker.Stop()
		delete(m.aggregators, n)
	}

	m.config = nil
	m.status.UpdateRe("mm", "Stopped", cmd)

	return nil
}

// @goroutine[0]
func (m *Manager) IsRunning() bool {
	if m.config != nil {
		return true
	}
	return false
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) error {
	defer m.status.Update("mm", "Ready")

	// Agent should check IsRunning() and only call if true,
	// else return SerivceIsNotRunningError on our behalf.

	// Data contains name of sub-service (monitor) and its config.
	mm := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, mm); err != nil {
		return err
	}

	// Agent doesn't know which monitors we have; only we know.
	monitor, haveMonitor := m.monitors[mm.Name]
	if !haveMonitor {
		return errors.New("Unknown monitor: " + mm.Name)
	}

	// Start or stop the monitor.
	var err error
	switch cmd.Cmd {
	case "Start":
		m.status.UpdateRe("mm", "Starting "+mm.Name+" monitor", cmd)
		interval := m.config.Intervals[mm.Name]
		m.collectTickers[mm.Name] = m.tickerFactory.Make(interval.Collect)
		collectionChan := m.aggregators[interval.Report].collectionChan
		err = monitor.Start(mm.Config, m.collectTickers[mm.Name], collectionChan)
	case "Stop":
		m.status.UpdateRe("mm", "Stopping "+mm.Name+" monitor", cmd)
		err = monitor.Stop()
		m.collectTickers[mm.Name].Stop()
		delete(m.collectTickers, mm.Name)
	default:
		err = pct.UnknownCmdError{Cmd: cmd.Cmd}
	}

	return err
}

// @goroutine[1]
func (m *Manager) Status() string {
	return m.status.Get("mm", true)
}
