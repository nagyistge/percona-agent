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
	SlowLogFile string
	Runtime time.Duration
	StartOffset uint64
	StopOffset uint64
	ExampleQueries bool
	cc *agent.ControlChannels
	ResultChan chan *Result
	DoneChan chan *Worker
}

type Result struct {
	stopOffset uint64
	dateFile string
}

func NewWorker(job *Job) *Worker {
	w := &Worker{
		job: job,
		log: agentLog.NewLogWriter(job.cc.LogChan, "qh-worker"),
	}
	return w
}

func (w *Worker) Run() {
}
