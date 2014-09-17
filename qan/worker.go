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
	status *pct.Status
}

func NewSlowLogWorker(logger *pct.Logger, name string) *SlowLogWorker {
	w := &SlowLogWorker{
		logger: logger,
		name:   name,
		status: pct.NewStatus([]string{name}),
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
	a := event.NewEventAggregator(query.Fingerprint, query.Id, job.ExampleQueries)

	// Misc runtime meta data.
	jobSize := job.EndOffset - job.StartOffset
	var runtime time.Duration
	var progress string
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

		a.AddEvent(event)
		/*
			switch err.(type) {
			case mysqlLog.MixedRateLimitsError:
				result.Error = err.Error()
				break EVENT_LOOP
			}
		*/
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
