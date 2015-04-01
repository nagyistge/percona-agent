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

package slowlog

import (
	"fmt"
	"os"
	"time"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/go-mysql/event"
	"github.com/percona/go-mysql/log"
	parser "github.com/percona/go-mysql/log/slow"
	"github.com/percona/go-mysql/query"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
)

type WorkerFactory interface {
	Make(name string, config qan.Config, mysqlConn mysql.Connector) *Worker
}

type RealWorkerFactory struct {
	logChan chan *proto.LogEntry
}

func NewRealWorkerFactory(logChan chan *proto.LogEntry) *RealWorkerFactory {
	f := &RealWorkerFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealWorkerFactory) Make(name string, config qan.Config, mysqlConn mysql.Connector) *Worker {
	return NewWorker(pct.NewLogger(f.logChan, name), config, mysqlConn)
}

// --------------------------------------------------------------------------

type Job struct {
	Id             string
	SlowLogFile    string
	RunTime        time.Duration
	StartOffset    int64
	EndOffset      int64
	ExampleQueries bool
}

func (j *Job) String() string {
	return fmt.Sprintf("%s %d-%d", j.SlowLogFile, j.StartOffset, j.EndOffset)
}

type Worker struct {
	logger    *pct.Logger
	config    qan.Config
	mysqlConn mysql.Connector
	// --
	ZeroRunTime bool // testing
	// --
	name            string
	status          *pct.Status
	queryChan       chan string
	fingerprintChan chan string
	errChan         chan interface{}
	doneChan        chan bool
	oldSlowLogs     map[int]string
	job             *Job
	sync            *pct.SyncChan
	running         bool
	logParser       log.LogParser
	// Diff against mysql tz and UTC. Used to calculate first_seen and last_seen
	tzDiffUTC time.Duration
}

func NewWorker(logger *pct.Logger, config qan.Config, mysqlConn mysql.Connector) *Worker {
	// By default replace numbers in words with ?
	query.ReplaceNumbersInWords = true

	tzDiffUTC, err := GetTZDiffUTC(mysqlConn)
	if err != nil {
		logger.Warn(err.Error())
	}
	fmt.Println("here tz", tzDiffUTC)
	name := logger.Service()
	w := &Worker{
		logger:    logger,
		config:    config,
		mysqlConn: mysqlConn,
		// --
		name:            name,
		status:          pct.NewStatus([]string{name}),
		queryChan:       make(chan string, 1),
		fingerprintChan: make(chan string, 1),
		errChan:         make(chan interface{}, 1),
		doneChan:        make(chan bool, 1),
		oldSlowLogs:     make(map[int]string),
		sync:            pct.NewSyncChan(),
	}
	return w
}

func (w *Worker) Setup(interval *qan.Interval) error {
	w.logger.Debug("Setup:call")
	defer w.logger.Debug("Setup:return")
	w.logger.Debug("Setup:", interval)
	if interval.EndOffset >= w.config.MaxSlowLogSize {
		w.logger.Info(fmt.Sprintf("Rotating slow log: %s >= %s",
			pct.Bytes(uint64(interval.EndOffset)),
			pct.Bytes(uint64(w.config.MaxSlowLogSize))))
		if err := w.rotateSlowLog(interval); err != nil {
			w.logger.Error(err)
		}
	}
	w.job = &Job{
		Id:             fmt.Sprintf("%d", interval.Number),
		SlowLogFile:    interval.Filename,
		StartOffset:    interval.StartOffset,
		EndOffset:      interval.EndOffset,
		RunTime:        time.Duration(w.config.WorkerRunTime) * time.Second,
		ExampleQueries: w.config.ExampleQueries,
	}
	w.logger.Debug("Setup:", w.job)

	return nil
}

func (w *Worker) Run() (*qan.Result, error) {
	w.logger.Debug("Run:call")
	defer w.logger.Debug("Run:return")

	w.status.Update(w.name, "Starting job "+w.job.Id)
	defer w.status.Update(w.name, "Idle")

	stopped := false
	w.running = true
	defer func() {
		if stopped {
			w.sync.Done()
		}
		w.running = false
	}()

	// Open the slow log file. Be sure to close it else we'll leak fd.
	file, err := os.Open(w.job.SlowLogFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a slow log parser and run it.  It sends log.Event via its channel.
	// Be sure to stop it when done, else we'll leak goroutines.
	result := &qan.Result{}
	opts := log.Options{
		StartOffset: uint64(w.job.StartOffset),
		FilterAdminCommand: map[string]bool{
			"Binlog Dump":      true,
			"Binlog Dump GTID": true,
		},
	}
	p := w.MakeLogParser(file, opts)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				errMsg := fmt.Sprintf("Slow log parser for %s crashed: %s", w.job, err)
				w.logger.Error(errMsg)
				result.Error = errMsg
			}
		}()
		if err := p.Start(); err != nil {
			w.logger.Warn(err)
			result.Error = err.Error()
		}
	}()
	defer p.Stop()

	// Make an event aggregate to do all the heavy lifting: fingerprint
	// queries, group, and aggregate.
	a := event.NewEventAggregator(w.job.ExampleQueries, w.tzDiffUTC)

	// Misc runtime meta data.
	jobSize := w.job.EndOffset - w.job.StartOffset
	runtime := time.Duration(0)
	progress := "Not started"
	rateType := ""
	rateLimit := uint(0)

	// Do fingerprinting in a separate Go routine so we can recover in case
	// query.Fingerprint() crashes. We don't want one bad fingerprint to stop
	// parsing the entire interval. Also, we want to log crashes and hopefully
	// fix the fingerprinter.
	go w.fingerprinter()
	defer func() { w.doneChan <- true }()

	t0 := time.Now()
EVENT_LOOP:
	for event := range p.EventChan() {
		runtime = time.Now().Sub(t0)
		progress = fmt.Sprintf("%.1f%% %d/%d %d %.1fs",
			float64(event.Offset)/float64(w.job.EndOffset)*100, event.Offset, w.job.EndOffset, jobSize, runtime.Seconds())
		w.status.Update(w.name, fmt.Sprintf("Parsing %s: %s", w.job.SlowLogFile, progress))

		// Stop if Stop() called.
		select {
		case <-w.sync.StopChan:
			w.logger.Debug("Run:stop")
			stopped = true
			break EVENT_LOOP
		default:
		}

		// Stop if runtime exceeded.
		if runtime >= w.job.RunTime {
			errMsg := fmt.Sprintf("Timeout parsing %s: %s", w.job, progress)
			w.logger.Warn(errMsg)
			result.Error = errMsg
			break EVENT_LOOP
		}

		// Stop if past file end offset. This happens often because we parse
		// only a slice of the slow log, and it's growing (if MySQL is busy),
		// so typical case is, for example, parsing from offset 100 to 5000
		// but slow log is already 7000 bytes large and growing. So the first
		// event with offset > 5000 marks the end (StopOffset) of this slice.
		if int64(event.Offset) >= w.job.EndOffset {
			result.StopOffset = int64(event.Offset)
			break EVENT_LOOP
		}

		// Stop if rate limits are mixed. This shouldn't happen. If it does,
		// another program or person might have reconfigured the rate limit.
		// We don't handle by design this because it's too much of an edge case.
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

		// Fingerprint the query and add it to the event aggregator. If the
		// fingerprinter crashes, start it again and skip this event.
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

	// If StopOffset isn't set above it means we reached the end of the slow log
	// file. This happens if MySQL isn't busy so the slow log didn't grow any,
	// or we rotated the slow log in Setup() so we're finishing the rotated slow
	// log file. So the StopOffset is the end of the file which we're already
	// at, so use SEEK_CUR.
	if result.StopOffset == 0 {
		result.StopOffset, _ = file.Seek(0, os.SEEK_CUR)
	}

	// Finalize the global and class metrics, i.e. calculate metric stats.
	w.status.Update(w.name, "Finalizing job "+w.job.Id)
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
	if !w.ZeroRunTime {
		result.RunTime = time.Now().Sub(t0).Seconds()
	}

	w.logger.Info(fmt.Sprintf("Parsed %s: %s", w.job, progress))
	return result, nil
}

func (w *Worker) Stop() error {
	w.logger.Debug("Stop:call")
	defer w.logger.Debug("Stop:return")
	if w.running {
		w.sync.Stop()
		w.sync.Wait()
	}
	return nil
}

func (w *Worker) Cleanup() error {
	w.logger.Debug("Cleanup:call")
	defer w.logger.Debug("Cleanup:return")
	for i, file := range w.oldSlowLogs {
		w.status.Update(w.name, "Removing slow log "+file)
		if err := os.Remove(file); err != nil {
			w.logger.Warn(err)
			continue
		}
		delete(w.oldSlowLogs, i)
		w.logger.Info("Removed " + file)
	}
	return nil
}

func (w *Worker) Status() map[string]string {
	return w.status.All()
}

func (w *Worker) SetLogParser(p log.LogParser) {
	// This is just for testing, so tests can inject a parser that does
	// abnormal things like be slow, crash, etc.
	w.logParser = p
}

func (w *Worker) MakeLogParser(file *os.File, opts log.Options) log.LogParser {
	if w.logParser != nil {
		p := w.logParser
		w.logParser = nil
		return p
	}
	return parser.NewSlowLogParser(file, opts)
}

// --------------------------------------------------------------------------

func (w *Worker) fingerprinter() {
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

func (w *Worker) rotateSlowLog(interval *qan.Interval) error {
	w.logger.Debug("rotateSlowLog:call")
	defer w.logger.Debug("rotateSlowLog:return")

	w.status.Update(w.name, "Rotating slow log")
	defer w.status.Update(w.name, "Idle")

	if err := w.mysqlConn.Connect(2); err != nil {
		return err
	}
	defer w.mysqlConn.Close()

	// Stop slow log so we don't move it while MySQL is using it.
	if err := w.mysqlConn.Set(w.config.Stop); err != nil {
		return err
	}

	// Move current slow log by renaming it.
	newSlowLogFile := fmt.Sprintf("%s-%d", interval.Filename, time.Now().UTC().Unix())
	if err := os.Rename(interval.Filename, newSlowLogFile); err != nil {
		return err
	}

	// Re-enable slow log.
	if err := w.mysqlConn.Set(w.config.Start); err != nil {
		return err
	}

	// Modify interval so worker parses the rest of the old slow log.
	interval.Filename = newSlowLogFile
	interval.EndOffset, _ = pct.FileSize(newSlowLogFile) // todo: handle err

	// Save old slow log and remove later if configured to do so.
	if w.config.RemoveOldSlowLogs {
		w.oldSlowLogs[interval.Number] = newSlowLogFile
	}

	return nil
}

func GetTZDiffUTC(mysqlConn mysql.Connector) (time.Duration, error) {
	var tzDiffUTC time.Duration
	var mysqlNow time.Time
	if mysqlConn == nil {
		return 0, fmt.Errorf("cannot get time diff against UTC. No MySQL connection")
	}
	// FIXME
	if mysqlConn.DB() == nil {
		fmt.Println("here 0")
		err := mysqlConn.Connect(1)
		if err != nil {
			fmt.Println("err ", err)
			return 0, err
		}
		//defer mysqlConn.Close()
	}
	// Can still be nil in tests with a mocked DB (NulllMysql)
	if mysqlConn.DB() != nil {
		// FIXME
		fmt.Println("here 1")
		err := mysqlConn.DB().QueryRow("SELECT NOW()").Scan(&mysqlNow)
		if err != nil {
			return 0, err
		}
		err = mysqlConn.DB().Close()
		if err != nil {
			fmt.Printf("error closing %v\n", err)
		}
		//FIXME
		mysqlNow = time.Now().UTC()
		tzDiffUTC = time.Now().UTC().Truncate(time.Hour).Sub(mysqlNow.Truncate(time.Hour))
	}
	return tzDiffUTC, nil
}
