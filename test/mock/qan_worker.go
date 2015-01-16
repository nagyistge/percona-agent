/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

type QanWorker struct {
	name     string
	stopChan chan bool
	result   *qan.Result
	err      error
	crash    bool
	// --
	runningChan chan bool
}

func NewQanWorker(name string, stopChan chan bool, result *qan.Result, err error, crash bool) *QanWorker {
	w := &QanWorker{
		name:        name,
		stopChan:    stopChan,
		result:      result,
		err:         err,
		crash:       crash,
		runningChan: make(chan bool, 1),
	}
	return w
}

func (w *QanWorker) Setup(*qan.Interval) error {
	return nil
}

func (w *QanWorker) Run() (*qan.Result, error) {
	w.runningChan <- true

	if w.crash {
		panic(w.name)
	}

	// Pretend like we're running until test says to stop.
	<-w.stopChan

	return w.result, w.err
}

func (w *QanWorker) Stop() error {
	return nil
}

func (w *QanWorker) Cleanup() error {
	return nil
}

func (w *QanWorker) Status() map[string]string {
	return map[string]string{
		"qan-worker": "ok",
	}
}
