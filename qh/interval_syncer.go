package qh

import (
	"math"
	"time"
)

type IntervalSyncer struct {
	intervalSecond uint
	sleepFunc func(time.Duration)
}

func NewIntervalSyncer(intervalSecond uint, sleepFunc func(time.Duration)) *IntervalSyncer {
	s := &IntervalSyncer{
		intervalSecond: intervalSecond,
		sleepFunc: sleepFunc,
	}
	return s
}

func (s *IntervalSyncer) Sync(nowNanosecond float64) *time.Ticker {
	// n := float64(t.UnixNano())
	i := float64(time.Duration(s.intervalSecond) * time.Second)
	d := i - math.Mod(nowNanosecond, i)
	s.sleepFunc(time.Duration(d) * time.Nanosecond)
	ticker := time.NewTicker(time.Duration(s.intervalSecond) * time.Second)
	return ticker
}
