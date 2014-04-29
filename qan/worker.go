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
	"github.com/percona/cloud-tools/pct"
	mysqlLog "github.com/percona/mysql-log-parser/log"
	"github.com/percona/mysql-log-parser/log/parser"
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

type Result struct {
	StopOffset int64
	RunTime    float64
	Error      string `json:",omitempty"`
	Global     *mysqlLog.GlobalClass
	Classes    []*mysqlLog.QueryClass
}

type Worker interface {
	Name() string
	Status() string
	Run(job *Job) (*Result, error)
}

type WorkerFactory interface {
	Make(name string) Worker
}

// --------------------------------------------------------------------------

type SlowLogWorkerFactory struct {
	logChan chan *proto.LogEntry
}

func NewSlowLogWorkerFactory(logChan chan *proto.LogEntry) *SlowLogWorkerFactory {
	f := &SlowLogWorkerFactory{
		logChan: logChan,
	}
	return f
}

func (f *SlowLogWorkerFactory) Make(name string) Worker {
	return NewSlowLogWorker(pct.NewLogger(f.logChan, "qan-worker"), name)
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

	// Open the slow log file.
	file, err := os.Open(job.SlowLogFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a slow log parser and run it.  It sends events log events
	// via its channel.
	stopChan := make(chan bool, 1)
	opts := parser.Options{
		StartOffset: uint64(job.StartOffset),
		FilterAdminCommand: map[string]bool{
			"Binlog Dump":      true,
			"Binlog Dump GTID": true,
		},
	}
	p := parser.NewSlowLogParser(file, stopChan, opts)
	go p.Run()

	// The global class has info and stats for all events.
	// Each query has its own class, defined by the checksum of its fingerprint.
	result := &Result{}
	global := mysqlLog.NewGlobalClass()
	queries := make(map[string]*mysqlLog.QueryClass)
	t0 := time.Now()
	jobSize := job.EndOffset - job.StartOffset
	var runtime time.Duration

EVENT_LOOP:
	for event := range p.EventChan {
		// Check run time, stop if exceeded.
		runtime = time.Now().Sub(t0)
		if runtime >= job.RunTime {
			result.Error = "Run-time timeout: " + job.RunTime.String()
			stopChan <- true
			break EVENT_LOOP
		}

		w.status.Update(w.name, fmt.Sprintf("Parsing %s: %.1f%% %d/%d %d %.1fs",
			job.SlowLogFile, float64(event.Offset)/float64(job.EndOffset)*100, event.Offset, job.EndOffset, jobSize, runtime.Seconds()))

		if int64(event.Offset) >= job.EndOffset {
			result.StopOffset = int64(event.Offset)
			break
		}

		// Add the event to the global class.
		err := global.AddEvent(event)
		switch err.(type) {
		case mysqlLog.MixedRateLimitsError:
			result.Error = err.Error()
			stopChan <- true
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
	return result, nil
}
