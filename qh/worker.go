package qh

import (
	"time"
	"github.com/percona/percona-cloud-tools/agent"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
//	mysqlLog "github.com/percona/percona-go-mysql/log"
//	"github.com/percona/percona-go-mysql/log/parser"
)

type QhJob struct {
	Slowlog string
	Runtime time.Duration
	StartOffset uint64
	StopOffset uint64
	ExampleQueries bool
}

type QhWorker struct {
	cc *agent.ControlChannels
	job *QhJob
	log *agentLog.LogWriter
}

func NewQhWorker(cc *agent.ControlChannels, job *QhJob) *QhWorker {
	w := &QhWorker{
		cc: cc,
		job: job,
		log: agentLog.NewLogWriter(cc.LogChan, "qh-worker"),
	}
	return w
}

func (w *QhWorker) Run() {
}
