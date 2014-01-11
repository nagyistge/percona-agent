package mm

import (
	"github.com/percona/cloud-tools/pct"
)

/**
 * A Monitor collects one or more Metric, usually many.  The MySQL monitor
 * (mysql/monitor.go) collects most SHOW STATUS and SHOW VARIABLE variables,
 * each as its own Metric.  Instead of sending metrics one-by-one to the
 * Aggregator (aggregator.go), they're sent as a Collection. The Aggregator
 * keeps Stats for each Metric in the Collection.  When the interval ends,
 * a Report is sent to a data spool (../data/sender.go).
 */

// Using given config, collect metrics when tickerChan ticks, and send to collecitonChan.
type Monitor interface {
	Start(config []byte, ticker pct.Ticker, collectionChan chan *Collection) error
	Stop() error
	Status() string
}

type Metric struct {
	Name  string  // mysql/status/Threads_running
	Value float64 // 13
}

type Collection struct {
	StartTs int64
	Metrics []Metric
}

type Report struct {
	StartTs  int64
	Metrics  map[string]*Stats
}
