package agent

import (
	"github.com/percona/percona-cloud-tools/agent/log"
)

type ControlChannels struct {
	LogChan chan *log.LogEntry
	StopChan chan bool
	DoneChan chan bool
}
