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

package mock

import (
	"github.com/percona/percona-agent/qan"
)

type QanWorkerFactory struct {
	workers  []*QanWorker
	workerNo int
}

func NewQanWorkerFactory(workers []*QanWorker) qan.WorkerFactory {
	f := &QanWorkerFactory{
		workers: workers,
	}
	return f
}

func (f *QanWorkerFactory) Make(name string) qan.Worker {
	if f.workerNo > len(f.workers) {
		return f.workers[f.workerNo-1]
	}
	nextWorker := f.workers[f.workerNo]
	f.workerNo++
	return nextWorker
}

type QanWorker struct {
	name     string
	stopChan chan bool
	result   *qan.Result
	err      error
	// --
	runningChan chan bool
	Job         *qan.Job
}

func NewQanWorker(name string, stopChan chan bool, result *qan.Result, err error) *QanWorker {
	w := &QanWorker{
		name:        name,
		stopChan:    stopChan,
		result:      result,
		err:         err,
		runningChan: make(chan bool, 1),
	}
	return w
}

func (w *QanWorker) Name() string {
	return w.name
}

func (w *QanWorker) Status() string {
	return "ok"
}

func (w *QanWorker) Run(job *qan.Job) (*qan.Result, error) {
	w.Job = job
	w.runningChan <- true

	// Pretend like we're running until test says to stop.
	<-w.stopChan

	return w.result, w.err
}

func (w *QanWorker) Running() chan bool {
	return w.runningChan
}
