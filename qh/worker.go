package qh

import (
	"os"
	"time"
	"fmt"
	"github.com/percona/percona-cloud-tools/agent"
	agentLog "github.com/percona/percona-cloud-tools/agent/log"
//	mysqlLog "github.com/percona/percona-go-mysql/log"
	"github.com/percona/percona-go-mysql/log/parser"
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
	Error error
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
	// Whenever and however we return, send qh-manager our result and
	// tell it we're done so it frees our spot for another concurrent
	// worker.
	result := new(Result)
	defer func() {
		w.job.ResultChan <- result
		w.job.DoneChan <- w
	}()

	// Open the slow log file.
	file, err := os.Open(w.job.SlowLogFile)
	if err != nil {
		result.Error = err
		return
	}

	// Create a slow log parser and run it.  It sends events log events
	// via its channel.
	p := parser.NewSlowLogParser(file, true) // false=debug off
	if err != nil {
		result.Error = err
		return
	}
	go p.Run()

	for e := range p.EventChan {
		fmt.Println(e)
	}

	return
}
