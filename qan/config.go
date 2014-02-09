package qan

import (
	"github.com/percona/cloud-tools/mysql"
)

const CONFIG_FILE = "qan.conf"

type Config struct {
	// Manager
	DSN               string
	Start             []mysql.Query
	Stop              []mysql.Query
	MaxWorkers        int
	Interval          uint  // minutes, "How often to report"
	MaxSlowLogSize    int64 // bytes, 0 = no max
	RemoveOldSlowLogs bool  // after rotating for MaxSlowLogSize
	// Worker
	ExampleQueries bool // only fingerprints if false
	WorkerRunTime  uint // seconds
}
