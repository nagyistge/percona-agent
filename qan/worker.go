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
	mysqlLog "github.com/percona/percona-go-mysql/log"
	"github.com/percona/percona-go-mysql/log/parser"
	"os"
	"time"
)

type Job struct {
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
	Run(job *Job) (*Result, error)
}

type WorkerFactory interface {
	Make() Worker
}

type SlowLogWorkerFactory struct {
}

func (f *SlowLogWorkerFactory) Make() Worker {
	return NewSlowLogWorker()
}

type SlowLogWorker struct {
}

func NewSlowLogWorker() Worker {
	w := &SlowLogWorker{}
	return w
}

func (w *SlowLogWorker) Run(job *Job) (*Result, error) {

	// Open the slow log file.
	file, err := os.Open(job.SlowLogFile)
	if err != nil {
		return nil, err
	}

	// Seek to the start offset, if any.
	// @todo error if start off > file size
	if job.StartOffset != 0 {
		// @todo handle error
		file.Seek(int64(job.StartOffset), os.SEEK_SET)
	}

	// Create a slow log parser and run it.  It sends events log events
	// via its channel.
	stopChan := make(chan bool, 1)
	opts := parser.Options{
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
EVENT_LOOP:
	for event := range p.EventChan {
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
			class = mysqlLog.NewQueryClass(classId, fingerprint)
			queries[classId] = class
		}

		// Add the event to its query class.
		class.AddEvent(event)

		// Check run time, stop if exceeded.
		if time.Now().Sub(t0) >= job.RunTime {
			result.Error = "Run-time timeout: " + job.RunTime.String()
			stopChan <- true
			break EVENT_LOOP
		}
	}

	if result.StopOffset == 0 {
		result.StopOffset, _ = file.Seek(0, os.SEEK_CUR)
	}

	// Done parsing the slow log.  Finalize the global and query classes (calculate
	// averages, etc.).
	for _, class := range queries {
		class.Finalize()
	}
	global.Finalize(uint64(len(queries)))

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

	return result, nil
}
