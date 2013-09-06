package qh

import (
	"time"
	"github.com/percona/percona-cloud-tools/agent"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
//	mysqlLog "github.com/percona/percona-go-mysql/log"
//	"github.com/percona/percona-go-mysql/log/parser"
)

type Worker struct {
	cc *agent.ControlChannels
	job *Job
	log *agentLog.LogWriter
}

type Job struct {
	Slowlog string
	Runtime time.Duration
	StartOffset uint64
	StopOffset uint64
	ExampleQueries bool
	resultChan *Result
	doneChan *Worker
}

type Result struct {
	stopOffset uint64
	dateFile string
}

func NewWorker(cc *agent.ControlChannels, job *Job) *Worker {
	w := &Worker{
		cc: cc,
		job: job,
		log: agentLog.NewLogWriter(cc.LogChan, "qh-worker"),
	}
	return w
}

func (w *Worker) Run() {
}
