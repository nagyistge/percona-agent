package mock

/**
 * Implements ticker.Ticker interface
 */

import (
	"time"
)

type Ticker struct {
	syncChan    chan bool
	Added       []chan time.Time
	RunningChan chan bool
}

func NewTicker(syncChan chan bool) *Ticker {
	t := &Ticker{
		syncChan:    syncChan,
		Added:       []chan time.Time{},
		RunningChan: make(chan bool),
	}
	return t
}

func (t *Ticker) Run(now int64) {
	if t.syncChan != nil {
		<-t.syncChan
	}
	t.RunningChan <- true
	return
}

func (t *Ticker) Stop() {
	t.RunningChan <- false
	return
}

func (t *Ticker) Add(c chan time.Time) {
	t.Added = append(t.Added, c)
}

func (t *Ticker) Remove(c chan time.Time) {
}

func (t *Ticker) ETA(now int64) float64 {
	return 0.1
}
