package qan

import (
	pct "github.com/percona/cloud-tools"
	"os"
	"time"
)

// Each QAN interval is a slice of the MySQL slow log:
type Interval struct {
	Filename    string
	StartTime   time.Time
	StopTime    time.Time
	StartOffset int64
	StopOffset  int64
}

type IntervalIter interface {
	Start()
	Stop()
	IntervalChan() chan *Interval
}

type FileIntervalIter struct {
	fileName     func() (string, error)
	tickerChan   chan time.Time
	intervalChan chan *Interval
	sync         *pct.SyncChan
	running      bool
}

func NewFileIntervalIter(fileName func() (string, error), tickerChan chan time.Time) *FileIntervalIter {
	iter := &FileIntervalIter{
		fileName:     fileName,
		tickerChan:   tickerChan,
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

	cur := new(Interval)
	for {
		select {
		case <-i.sync.StopChan:
			return
		case now := <-i.tickerChan:
			// Get the MySQL slow log file name at each interval because it can change.
			curFile, err := i.fileName()
			if err != nil {
				cur = new(Interval)
				continue
			}

			// Get the current size of the MySQL slow log.
			curSize, err := fileSize(curFile)
			if err != nil {
				cur = new(Interval)
				continue
			}

			if !cur.StartTime.IsZero() { // StartTime is set
				// End of interval
				cur.Filename = curFile
				cur.StopOffset = curSize
				cur.StopTime = now

				// Send interval non-blocking: if reciever is not ready,
				// that's ok, the system may be busy, so drop the interval.
				// todo: count dropped intervals
				select {
				case i.intervalChan <- cur:
				default:
				}

				// Start of next interval
				cur := new(Interval)
				cur.StartOffset = curSize
				cur.StartTime = now
			} else {
				// First interval, either due to first tick or because an error
				// occurred earlier so a new interval was started.
				cur.StartOffset = curSize
				cur.StartTime = now
			}
		}
	}
}

func fileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}
