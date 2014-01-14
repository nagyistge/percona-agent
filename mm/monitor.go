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
	Status() map[string]string
}

type Metric struct {
	Name   string  // mysql/status/Threads_running
	Type   byte    // see below
	Number float64 // Type=NUMBER|COUNTER
	String string  // Type=STRING
}

/**
 * Metric.Type is one of:
 *    NUMBER: standard metric type for which we calc full Stats: pct5, min, med, etc.
 *   COUNTER: value only increases or decreases; we only calc rate; e.g. Bytes_sent
 *    STRING: value is a string, used to collect config/setting values
 */
const (
	_ byte = iota
	NUMBER
	COUNTER
	STRING
)

type Collection struct {
	StartTs int64
	Metrics []Metric
}

type Report struct {
	StartTs int64
	Metrics map[string]*Stats
}
