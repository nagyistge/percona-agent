/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package qan

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/go-mysql/event"
	"github.com/percona/go-mysql/log"
	parser "github.com/percona/go-mysql/log/slow"
	"github.com/percona/go-mysql/query"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"os"
	"time"
)

type Job struct {
	Id             string
	SlowLogFile    string
	RunTime        time.Duration
	StartOffset    int64
	EndOffset      int64
	ExampleQueries bool
	// --
	ZeroRunTime bool // testing
}

func (j *Job) String() string {
	return fmt.Sprintf("%s %d-%d", j.SlowLogFile, j.StartOffset, j.EndOffset)
}

type Worker interface {
	Name() string
	Status() string
	Run(job *Job) (*Result, error)
}

type WorkerFactory interface {
	Make(collectFrom, name string, mysqlConn mysql.Connector) Worker
}

// --------------------------------------------------------------------------

type RealWorkerFactory struct {
	logChan chan *proto.LogEntry
}

func NewRealWorkerFactory(logChan chan *proto.LogEntry) *RealWorkerFactory {
	f := &RealWorkerFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealWorkerFactory) Make(collectFrom, name string, mysqlConn mysql.Connector) Worker {
	switch collectFrom {
	case "slowlog":
		return NewSlowLogWorker(pct.NewLogger(f.logChan, "qan-worker"), name)
	case "perfschema":
		return NewPfsWorker(pct.NewLogger(f.logChan, "qan-worker"), name, mysqlConn)
	}
	return nil
}

// --------------------------------------------------------------------------

type SlowLogWorker struct {
	logger *pct.Logger
	name   string
	// --
	status          *pct.Status
	queryChan       chan string
	fingerprintChan chan string
	errChan         chan interface{}
	doneChan        chan bool
}

func NewSlowLogWorker(logger *pct.Logger, name string) *SlowLogWorker {
	w := &SlowLogWorker{
		logger: logger,
		name:   name,
		// --
		status:          pct.NewStatus([]string{name}),
		queryChan:       make(chan string, 1),
		fingerprintChan: make(chan string, 1),
		errChan:         make(chan interface{}, 1),
		doneChan:        make(chan bool, 1),
	}
	return w
}

func (w *SlowLogWorker) Name() string {
	return w.name
}

func (w *SlowLogWorker) Status() string {
	return w.status.Get(w.name)
}

func (w *SlowLogWorker) Run(job *Job) (*Result, error) {
	w.logger.Debug("Run:call")
	defer w.logger.Debug("Run:return")

	w.status.Update(w.name, "Starting job "+job.Id)
	result := &Result{}

	// Open the slow log file.
	file, err := os.Open(job.SlowLogFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a slow log parser and run it.  It sends log.Event via its channel.
	// Be sure to stop it when done, else we'll leak goroutines.
	opts := log.Options{
		StartOffset: uint64(job.StartOffset),
		FilterAdminCommand: map[string]bool{
			"Binlog Dump":      true,
			"Binlog Dump GTID": true,
		},
	}
	p := parser.NewSlowLogParser(file, opts)
	defer p.Stop()
	go func() {
		defer func() {
			if err := recover(); err != nil {
				errMsg := fmt.Sprintf("Slow log parser for %s crashed: %s", job, err)
				w.logger.Error(errMsg)
				result.Error = errMsg
			}
		}()
		if err := p.Start(); err != nil {
			w.logger.Warn(err)
			result.Error = err.Error()
		}
	}()

	// Make an event aggregate to do all the heavy lifting: fingerprint
	// queries, group, and aggregate.
	a := event.NewEventAggregator(job.ExampleQueries)

	// Misc runtime meta data.
	jobSize := job.EndOffset - job.StartOffset
	runtime := time.Duration(0)
	progress := "Not started"
	rateType := ""
	rateLimit := uint(0)

	go w.fingerprinter()
	defer func() { w.doneChan <- true }()

	t0 := time.Now()
EVENT_LOOP:
	for event := range p.EventChan() {
		runtime = time.Now().Sub(t0)
		progress = fmt.Sprintf("%.1f%% %d/%d %d %.1fs",
			float64(event.Offset)/float64(job.EndOffset)*100, event.Offset, job.EndOffset, jobSize, runtime.Seconds())
		w.status.Update(w.name, fmt.Sprintf("Parsing %s: %s", job.SlowLogFile, progress))

		// Check runtime, stop if exceeded.
		if runtime >= job.RunTime {
			errMsg := fmt.Sprintf("Timeout parsing %s: %s", progress)
			w.logger.Warn(errMsg)
			result.Error = errMsg
			break EVENT_LOOP
		}

		if int64(event.Offset) >= job.EndOffset {
			result.StopOffset = int64(event.Offset)
			break EVENT_LOOP
		}

		if event.RateType != "" {
			if rateType != "" {
				if rateType != event.RateType || rateLimit != event.RateLimit {
					errMsg := fmt.Sprintf("Slow log has mixed rate limits: %s/%d and %s/%d",
						rateType, rateLimit, event.RateType, event.RateLimit)
					w.logger.Warn(errMsg)
					result.Error = errMsg
					break EVENT_LOOP
				}
			} else {
				rateType = event.RateType
				rateLimit = event.RateLimit
			}
		}

		var fingerprint string
		w.queryChan <- event.Query
		select {
		case fingerprint = <-w.fingerprintChan:
			id := query.Id(fingerprint)
			a.AddEvent(event, id, fingerprint)
		case _ = <-w.errChan:
			w.logger.Warn(fmt.Sprintf("Cannot fingerprint '%s'", event.Query))
			go w.fingerprinter()
		}
	}

	if result.StopOffset == 0 {
		result.StopOffset, _ = file.Seek(0, os.SEEK_CUR)
	}

	// Finalize the global and class metrics, i.e. calculate metric stats.
	w.status.Update(w.name, "Finalizing job "+job.Id)
	r := a.Finalize()

	// The aggregator result is a map, but we need an array of classes for
	// the query report, so convert it.
	n := len(r.Class)
	classes := make([]*event.QueryClass, n)
	for _, class := range r.Class {
		n-- // can't classes[--n] in Go
		classes[n] = class
	}
	result.Global = r.Global
	result.Class = classes

	// Zero the runtime for testing.
	if !job.ZeroRunTime {
		result.RunTime = time.Now().Sub(t0).Seconds()
	}

	w.status.Update(w.name, "Done job "+job.Id)
	w.logger.Info(fmt.Sprintf("Parsed %s: %s", job, progress))
	return result, nil
}

func (w *SlowLogWorker) fingerprinter() {
	w.logger.Debug("fingerprinter:call")
	defer w.logger.Debug("fingerprinter:return")
	defer func() {
		if err := recover(); err != nil {
			w.errChan <- err
		}
	}()
	for {
		select {
		case q := <-w.queryChan:
			f := query.Fingerprint(q)
			w.fingerprintChan <- f
		case <-w.doneChan:
			return
		}
	}
}
