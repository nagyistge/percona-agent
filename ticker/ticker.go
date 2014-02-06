package ticker

import (
	"github.com/percona/cloud-tools/pct"
	"math"
	"sync"
	"time"
)

type Ticker interface {
	Run(nowNanosecond int64)
	Stop()
	Add(c chan time.Time)
	Remove(c chan time.Time)
}

type TickerFactory interface {
	Make(atInterval uint) Ticker
}

type EvenTicker struct {
	atInterval uint
	sleep      func(time.Duration)
	ticker     *time.Ticker
	watcher    map[chan time.Time]bool
	watcherMux *sync.Mutex
	sync       *pct.SyncChan
}

func NewEvenTicker(atInterval uint, sleep func(time.Duration)) *EvenTicker {
	et := &EvenTicker{
		atInterval: atInterval,
		sleep:      sleep,
		watcher:    make(map[chan time.Time]bool),
		watcherMux: new(sync.Mutex),
		sync:       pct.NewSyncChan(),
	}
	return et
}

func (et *EvenTicker) Run(nowNanosecond int64) {
	defer et.sync.Done()
	i := float64(time.Duration(et.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	et.sleep(time.Duration(d) * time.Nanosecond)
	et.ticker = time.NewTicker(time.Duration(et.atInterval) * time.Second)
	et.tick(time.Now().UTC()) // first tick
	for {
		select {
		case now := <-et.ticker.C:
			et.tick(now.UTC())
		case <-et.sync.StopChan:
			return
		}
	}
}

func (et *EvenTicker) Stop() {
	et.sync.Stop()
	et.sync.Wait()
	et.ticker.Stop()
	et.ticker = nil
}

func (et *EvenTicker) Add(c chan time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	if !et.watcher[c] {
		et.watcher[c] = true
	}
}

func (et *EvenTicker) Remove(c chan time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	if et.watcher[c] {
		delete(et.watcher, c)
	}
}

func (et *EvenTicker) tick(t time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	for c, _ := range et.watcher {
		select {
		case c <- t:
		default:
			// watcher missed this tick
		}
	}
}
