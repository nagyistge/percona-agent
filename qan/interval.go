package qan

import (
	"github.com/percona/cloud-tools/pct"
	"os"
	"time"
)

// A slice of the MySQL slow log:
type Interval struct {
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

// Implements IntervalIter:
type FileIntervalIter struct {
	filename FilenameFunc
	tickChan chan time.Time
	// --
	intervalChan chan *Interval
	sync         *pct.SyncChan
	running      bool
}

func NewFileIntervalIter(filename FilenameFunc, tickChan chan time.Time) *FileIntervalIter {
	iter := &FileIntervalIter{
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
		select {
		case now := <-i.tickChan:
			// Get the MySQL slow log file name at each interval because it can change.
			curFile, err := i.filename()
			if err != nil {
				cur = new(Interval)
				continue
			}

			// Get the current size of the MySQL slow log.
			curSize, err := pct.FileSize(curFile)
			if err != nil {
				cur = new(Interval)
				continue
			}

			// File changed if prev file not same as current file.
			// @todo: Normally this only changes when QAN manager rotates slow log
			//        at interval.  If it changes for another reason (e.g. user
			//        renames slow log) then StartOffset=0 may not be ideal.
			curFileInfo, _ := os.Stat(curFile)
			fileChanged := !os.SameFile(prevFileInfo, curFileInfo)
			prevFileInfo = curFileInfo

			if !cur.StartTime.IsZero() { // StartTime is set
				// End of current interval:
				cur.Filename = curFile
				if fileChanged {
					// Start from beginning of new file.
					cur.StartOffset = 0
				}
				cur.EndOffset = curSize
				cur.StopTime = now

				// Send interval non-blocking: if reciever is not ready,
				// that's ok, the system may be busy, so drop the interval.
				select {
				case i.intervalChan <- cur:
				default:
					// @todo: handle, count lost intervals
				}

				// Next interval:
				cur = &Interval{
					StartTime:   now,
					StartOffset: curSize,
				}
			} else {
				// First interval, either due to first tick or because an error
				// occurred earlier so a new interval was started.
				cur.StartOffset = curSize
				cur.StartTime = now
				prevFileInfo, _ = os.Stat(curFile)
			}
		case <-i.sync.StopChan:
			return
		}
	}
}
