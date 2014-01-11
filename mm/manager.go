package mm

import (
	"fmt"
	"encoding/json"
	"errors"
	proto "github.com/percona/cloud-protocol"
	"github.com/percona/cloud-tools/pct"
)

type Binding struct {
	ticker pct.Ticker
	collectionChan chan *Collection
	aggregator  *Aggregator
}

type Manager struct {
	logger   *pct.Logger
	monitors map[string]Monitor
	tickerFactory pct.TickerFactory
	dataChan chan interface{}
	// --
	config *Config // nil if not running
	status *pct.Status
	aggregators map[uint]*Binding

}

func NewManager(logger *pct.Logger, monitors map[string]Monitor, tickerFactory pct.TickerFactory, dataChan chan interface{}) *Manager {
	m := &Manager{
		logger:   logger,
		monitors: monitors,
		tickerFactory: tickerFactory,
		dataChan: dataChan,
		// --
		config: nil, // not running yet
		status: pct.NewStatus([]string{"mm"}),
		aggregators: make(map[uint]*Binding),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
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
			collectionChan := make(chan *Collection, 2 * len(c.Intervals))
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
	for name, monitor := range m.monitors {
		m.status.UpdateRe("mm", "Stopping "+name, cmd)
		monitor.Stop()
	}
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
	mm := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, mm); err != nil {
		return err
	}

	monitor, haveMonitor := m.monitors[mm.Name]
	if !haveMonitor {
		return errors.New("Unknown monitor: " + mm.Name)
	}

	var err error
	switch cmd.Cmd {
	case "Start":
		interval := m.config.Intervals[mm.Name]
		ticker := m.tickerFactory.Make(interval.Collect)
		collectionChan := m.aggregators[interval.Report].collectionChan

		err = monitor.Start(mm.Config, ticker, collectionChan)
	case "Stop":
		err = monitor.Stop()
	default:
		err = pct.UnknownCmdError{Cmd: cmd.Cmd}
	}

	return err
}

// @goroutine[1]
func (m *Manager) Status() string {
	return ""
}
