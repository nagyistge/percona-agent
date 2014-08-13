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
	mysqlLog "github.com/percona/mysql-log-parser/log"
	"github.com/percona/mysql-log-parser/log/parser"
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

	// Create a slow log parser and run it.  It sends events log events via its channel.
	// Be sure to stop it when done, else we'll leak goroutines.  stopChan must be buffered
	// so we don't block on send if parser crashes.
	stopChan := make(chan bool, 1)
	defer func() { stopChan <- true }()
	opts := parser.Options{
		StartOffset: uint64(job.StartOffset),
		FilterAdminCommand: map[string]bool{
			"Binlog Dump":      true,
			"Binlog Dump GTID": true,
		},
	}
	p := parser.NewSlowLogParser(file, stopChan, opts)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				errMsg := fmt.Sprintf("Slow log parser for %s crashed: %s", job, err)
				w.logger.Error(errMsg)
				result.Error = errMsg
			}
		}()
		p.Run()
	}()

	// The global class has info and stats for all events.
	// Each query has its own class, defined by the checksum of its fingerprint.
	global := mysqlLog.NewGlobalClass()
	queries := make(map[string]*mysqlLog.QueryClass)
	jobSize := job.EndOffset - job.StartOffset
	var runtime time.Duration
	var progress string
	t0 := time.Now()

EVENT_LOOP:
	for event := range p.EventChan {
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

		// Add the event to the global class.
		err := global.AddEvent(event)
		switch err.(type) {
		case mysqlLog.MixedRateLimitsError:
			result.Error = err.Error()
			break EVENT_LOOP
		}

		// Get the query class to which the event belongs.
		fingerprint := mysqlLog.Fingerprint(event.Query)
		classId := mysqlLog.Checksum(fingerprint)
		class, haveClass := queries[classId]
		if !haveClass {
			class = mysqlLog.NewQueryClass(classId, fingerprint, job.ExampleQueries)
			queries[classId] = class
		}

		// Add the event to its query class.
		class.AddEvent(event)
	}

	w.status.Update(w.name, "Finalizing job "+job.Id)

	if result.StopOffset == 0 {
		result.StopOffset, _ = file.Seek(0, os.SEEK_CUR)
	}

	// Done parsing the slow log.  Finalize the global and query classes (calculate
	// averages, etc.).
	for _, class := range queries {
		class.Finalize()
	}
	global.Finalize(uint64(len(queries)))

	// Sort the results, keep the top and combine the rest into a single class: Low-Ranking Queries (LRQ).
	w.status.Update(w.name, "Combining LRQ job "+job.Id)
	nQueries := len(queries)
	classes := make([]*mysqlLog.QueryClass, nQueries)
	for _, class := range queries {
		// Decr before use; can't classes[--nQueries] in Go.
		nQueries--
		classes[nQueries] = class
	}

	result.Global = global
	result.Classes = classes

	if !job.ZeroRunTime {
		result.RunTime = time.Now().Sub(t0).Seconds()
	}

	w.status.Update(w.name, "Done job "+job.Id)
	w.logger.Info(fmt.Sprintf("Parsed %s: %s", job, progress))
	return result, nil
}
