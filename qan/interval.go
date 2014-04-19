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
	"os"
	"time"
)

// A slice of the MySQL slow log:
type Interval struct {
	Number      int
	Filename    string    // slow_query_log_file
	StartTime   time.Time // UTC
	StopTime    time.Time // UTC
	StartOffset int64     // bytes @ StartTime
	EndOffset   int64     // bytes @ StopTime
}

// Returns slow_query_log_file, or error:
type FilenameFunc func() (string, error)

// Used by Manager.run() to start a Worker:
type IntervalIter interface {
	Start()
	Stop()
	IntervalChan() chan *Interval
}

// Used by Manager.Start() to create an IntervalIter that ticks at Config.Interval minutes:
type IntervalIterFactory interface {
	Make(filename FilenameFunc, tickChan chan time.Time) IntervalIter
}

type FileIntervalIterFactory struct {
	logChan chan *proto.LogEntry
}

func NewFileIntervalIterFactory(logChan chan *proto.LogEntry) *FileIntervalIterFactory {
	f := &FileIntervalIterFactory{
		logChan: logChan,
	}
	return f
}

func (f *FileIntervalIterFactory) Make(filename FilenameFunc, tickChan chan time.Time) IntervalIter {
	return NewFileIntervalIter(pct.NewLogger(f.logChan, "qan-interval"), filename, tickChan)
}

// Implements IntervalIter:
type FileIntervalIter struct {
	logger   *pct.Logger
	filename FilenameFunc
	tickChan chan time.Time
	// --
	intervalNo   int
	intervalChan chan *Interval
	sync         *pct.SyncChan
	running      bool
}

func NewFileIntervalIter(logger *pct.Logger, filename FilenameFunc, tickChan chan time.Time) *FileIntervalIter {
	iter := &FileIntervalIter{
		logger:   logger,
		filename: filename,
		tickChan: tickChan,
		// --
		intervalChan: make(chan *Interval, 1),
		running:      false,
		sync:         pct.NewSyncChan(),
	}
	return iter
}

func (i *FileIntervalIter) Start() {
	if i.running {
		return
	}
	go i.run()
}

func (i *FileIntervalIter) Stop() {
	i.sync.Stop()
	i.sync.Wait()
	return
}

func (i *FileIntervalIter) IntervalChan() chan *Interval {
	return i.intervalChan
}

func (i *FileIntervalIter) run() {
	defer func() {
		close(i.intervalChan)
		i.running = false
		i.sync.Done()
	}()

	var prevFileInfo os.FileInfo
	cur := &Interval{}

	for {
		i.logger.Debug("run:wait")

		select {
		case now := <-i.tickChan:
			i.logger.Debug("run:tick")

			// Get the MySQL slow log file name at each interval because it can change.
			curFile, err := i.filename()
			if err != nil {
				i.logger.Warn(err)
				cur = new(Interval)
				continue
			}

			// Get the current size of the MySQL slow log.
			i.logger.Debug("run:file size")
			curSize, err := pct.FileSize(curFile)
			if err != nil {
				i.logger.Warn(err)
				cur = new(Interval)
				continue
			}
			i.logger.Debug(fmt.Sprintf("run:%s:%d", curFile, curSize))

			// File changed if prev file not same as current file.
			// @todo: Normally this only changes when QAN manager rotates slow log
			//        at interval.  If it changes for another reason (e.g. user
			//        renames slow log) then StartOffset=0 may not be ideal.
			curFileInfo, _ := os.Stat(curFile)
			fileChanged := !os.SameFile(prevFileInfo, curFileInfo)
			prevFileInfo = curFileInfo

			if !cur.StartTime.IsZero() { // StartTime is set
				i.logger.Debug("run:next")
				i.intervalNo++

				// End of current interval:
				cur.Filename = curFile
				if fileChanged {
					// Start from beginning of new file.
					i.logger.Info("File changed")
					cur.StartOffset = 0
				}
				cur.EndOffset = curSize
				cur.StopTime = now
				cur.Number = i.intervalNo

				// Send interval to manager which should be ready to receive it.
				select {
				case i.intervalChan <- cur:
				case <-time.After(1 * time.Second):
					i.logger.Warn(fmt.Sprintf("Lost interval: %+v", cur))
				}

				// Next interval:
				cur = &Interval{
					StartTime:   now,
					StartOffset: curSize,
				}
			} else {
				// First interval, either due to first tick or because an error
				// occurred earlier so a new interval was started.
				i.logger.Debug("run:first")
				cur.StartOffset = curSize
				cur.StartTime = now
				prevFileInfo, _ = os.Stat(curFile)
			}
		case <-i.sync.StopChan:
			i.logger.Debug("run:stop")
			return
		}
	}
}
