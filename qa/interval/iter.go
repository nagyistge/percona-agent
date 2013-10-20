package interval

import (
	"os"
	"time"
)

type Iter interface {
	IntervalChan() chan *Interval
	Run()
}

type SyncTimeIter struct {
	fileName func() (string, error)
	tickerChan chan time.Time
	intervalChan chan *Interval
	stopChan chan bool
}

func NewSyncTimeIter(fileName func() (string, error), tickerChan chan time.Time, intervalChan chan *Interval, stopChan chan bool) *SyncTimeIter {
	iter := &SyncTimeIter{
		fileName: fileName,
		tickerChan: tickerChan,
		intervalChan: intervalChan,
		stopChan: stopChan,
	}
	return iter
}

func (i *SyncTimeIter) IntervalChan() chan *Interval {
	return i.intervalChan
}

func (i *SyncTimeIter) Run() {
	cur := new(Interval)
	for {
		select {
		case <-i.stopChan:
			break
		case now := <-i.tickerChan:
			curFile, err := i.fileName()
			if err != nil {
				cur = new(Interval)
				continue
			}

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

				i.intervalChan <-cur

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

func fileSize (fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}
