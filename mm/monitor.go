package mm

/**
 * A Monitor collects one or more Metric, usually many.  The MySQL monitor
 * (mysql/monitor.go) collects most SHOW STATUS and SHOW VARIABLE variables,
 * each as its own Metric.  Instead of sending metrics one-by-one to the
 * Aggregator (aggregator.go), they're sent as a Collection. The Aggregator
 * keeps Stats for each Metric in the Collection.  When the interval ends,
 * a Report is sent to a data spool (../data/sender.go).
 */

type Monitor interface {
	Start(config []byte) error // mysql/config.go
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
	Hostname string
	StartTs  int64
	Metrics  map[string]*Stats
}
