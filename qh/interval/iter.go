package interval

import (
	"os"
)

type Iter struct {
	cc *agent.ControlChannels
	intervalChan chan *qh.Interval
	file string
	fd uintptr
	stat os.FileInfo
}

func NewIter(cc *agent.ControlChannels, config Config, intervalChan chan *Interval, tickerChan bool) *Iter {
}

func (i *Iter) Run() {
	var interval *qh.Interval
	for t := range i.tickerChan {
		i.getSlowLogInfo()
		if interval != nil {
			i.intervalChan <-interval
			interval := new(qh.Interval)
		} else {
			fileInfo = fileInfo(cong
			interval = new(Interval)
			interval.StartTime = time.Now()
		}
	}
}

func (i *Iter) getSlowLogInfo() {
	f := os.NewFile(m.slowLogFd, m.slowLogFile)
	if fileInfo, err := f.Stat(); err != nil {
		return err
	} else {
		m.slowLogInfo = fileInfo
	}
	return nil
}
