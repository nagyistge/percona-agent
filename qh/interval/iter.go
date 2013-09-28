package interval

import (
	"os"
	"time"
)

type Iter struct {
	fileName func() (string, error)
	tickerChan chan time.Time
	intervalChan chan *Interval
	stopChan chan bool
	// --
	file string
	size int64
	fd uintptr
}

func NewIter(fileName func() (string, error), tickerChan chan time.Time, intervalChan chan *Interval, stopChan chan bool) *Iter {
	iter := &Iter{
		fileName: fileName,
		tickerChan: tickerChan,
		intervalChan: intervalChan,
		stopChan: stopChan,
	}
	return iter
}

func (i *Iter) Run() {
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

			if !cur.StartTime.IsZero() {
				// End of interval
				cur.FileName = curFile
				cur.StopOffset = curSize
				cur.StopTime = now

				i.intervalChan <-cur

				// Start of next interval
				cur := new(Interval)
				cur.StartOffset = curSize
				cur.StartTime = now
			} else {
				// First interval
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
