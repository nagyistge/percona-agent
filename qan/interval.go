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
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"os"
	"time"
)

// A slice of the MySQL slow log or a snapshot of the performanc schema:
type Interval struct {
	Number      int
	StartTime   time.Time // UTC
	StopTime    time.Time // UTC
	Filename    string    // slow_query_log_file
	StartOffset int64     // bytes @ StartTime
	EndOffset   int64     // bytes @ StopTime
}

func (i *Interval) String() string {
	t0 := i.StartTime.Format("2006-01-02 15:04:05 MST")
	t1 := i.StopTime.Format("2006-01-02 15:04:05 MST")
	if i.Filename != "" {
		return fmt.Sprintf("%d %s %s to %s (%d-%d)", i.Number, i.Filename, t0, t1, i.StartOffset, i.EndOffset)
	} else {
		return fmt.Sprintf("%d performance_schema %s to %s", i.Number, t0, t1)
	}
}

/////////////////////////////////////////////////////////////////////////////
// Interval iterator interface
/////////////////////////////////////////////////////////////////////////////

// Used by Manager.run() to start a Worker:
type IntervalIter interface {
	Start()
	Stop()
	IntervalChan() chan *Interval
}

/////////////////////////////////////////////////////////////////////////////
// Interval iterator factory
/////////////////////////////////////////////////////////////////////////////

// Returns slow_query_log_file, or error:
type FilenameFunc func() (string, error)

// Used by Manager.Start() to create an IntervalIter that ticks at Config.Interval minutes:
type IntervalIterFactory interface {
	Make(collectFrom string, filename FilenameFunc, tickChan chan time.Time) IntervalIter
}

type RealIntervalIterFactory struct {
	logChan chan *proto.LogEntry
}

func NewRealIntervalIterFactory(logChan chan *proto.LogEntry) *RealIntervalIterFactory {
	f := &RealIntervalIterFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealIntervalIterFactory) Make(collectFrom string, filename FilenameFunc, tickChan chan time.Time) IntervalIter {
	switch collectFrom {
	case "slowlog":
		return NewFileIntervalIter(pct.NewLogger(f.logChan, "qan-interval"), filename, tickChan)
	case "perfschema":
		return NewPfsIntervalIter(pct.NewLogger(f.logChan, "qan-interval"), tickChan)
	}
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Slow log iterator
/////////////////////////////////////////////////////////////////////////////

type FileIntervalIter struct {
	logger   *pct.Logger
	filename FilenameFunc
	tickChan chan time.Time
	// --
	intervalNo   int
	intervalChan chan *Interval
	sync         *pct.SyncChan
}

func NewFileIntervalIter(logger *pct.Logger, filename FilenameFunc, tickChan chan time.Time) *FileIntervalIter {
	iter := &FileIntervalIter{
		logger:   logger,
		filename: filename,
		tickChan: tickChan,
		// --
		intervalChan: make(chan *Interval, 1),
		sync:         pct.NewSyncChan(),
	}
	return iter
}

func (i *FileIntervalIter) Start() {
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
		i.sync.Done()
	}()

	var prevFileInfo os.FileInfo
	cur := &Interval{}

	for {
		i.logger.Debug("run:idle")

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

/////////////////////////////////////////////////////////////////////////////
// performance_schema iterator
/////////////////////////////////////////////////////////////////////////////

type PfsIntervalIter struct {
	logger    *pct.Logger
	msyqlConn mysql.Connector
	tickChan  chan time.Time
	// --
	intervalNo   int
	intervalChan chan *Interval
	sync         *pct.SyncChan
}

func NewPfsIntervalIter(logger *pct.Logger, tickChan chan time.Time) *PfsIntervalIter {
	iter := &PfsIntervalIter{
		logger:   logger,
		tickChan: tickChan,
		// --
		intervalChan: make(chan *Interval, 1),
		sync:         pct.NewSyncChan(),
	}
	return iter
}

func (i *PfsIntervalIter) Start() {
	go i.run()
}

func (i *PfsIntervalIter) Stop() {
	i.sync.Stop()
	i.sync.Wait()
	return
}

func (i *PfsIntervalIter) IntervalChan() chan *Interval {
	return i.intervalChan
}

func (i *PfsIntervalIter) run() {
	defer func() {
		i.sync.Done()
	}()

	cur := &Interval{}
	for {
		i.logger.Debug("run:wait")

		select {
		case now := <-i.tickChan:
			i.logger.Debug("run:tick")

			if !cur.StartTime.IsZero() { // StartTime is set
				i.logger.Debug("run:next")
				i.intervalNo++

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
					StartTime: now,
				}
			} else {
				// First interval, either due to first tick or because an error
				// occurred earlier so a new interval was started.
				i.logger.Debug("run:first")
				cur.StartTime = now
			}
		case <-i.sync.StopChan:
			i.logger.Debug("run:stop")
			return
		}
	}
}
