package eventicker

import (
	"math"
	"time"
)

type EvenTicker struct {
	atInterval uint
	sleep      func(time.Duration)
}

func NewEvenTicker(atInterval uint, sleep func(time.Duration)) *EvenTicker {
	et := &EvenTicker{
		atInterval: atInterval,
		sleep:      sleep,
	}
	return et
}

func (et *EvenTicker) Sync(nowNanosecond int64) *time.Ticker {
	i := float64(time.Duration(et.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	et.sleep(time.Duration(d) * time.Nanosecond)
	ticker := time.NewTicker(time.Duration(et.atInterval) * time.Second)
	return ticker
}
