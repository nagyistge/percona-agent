package pct

import (
	"math"
	"time"
)

type Ticker interface {
	Sync(nowNanosecond int64)
	TickerChan() <-chan time.Time
	Stop()
}

type TickerFactory interface {
	Make(atInterval uint) Ticker
}

type EvenTicker struct {
	atInterval uint
	sleep      func(time.Duration)
	ticker     *time.Ticker
}

func NewEvenTicker(atInterval uint, sleep func(time.Duration)) *EvenTicker {
	et := &EvenTicker{
		atInterval: atInterval,
		sleep:      sleep,
	}
	return et
}

func (et *EvenTicker) Sync(nowNanosecond int64) {
	i := float64(time.Duration(et.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	et.sleep(time.Duration(d) * time.Nanosecond)
	et.ticker = time.NewTicker(time.Duration(et.atInterval) * time.Second)
}

func (et *EvenTicker) TickerChan() <-chan time.Time {
	return et.ticker.C
}

func (et *EvenTicker) Stop() {
	et.ticker.Stop()
}
