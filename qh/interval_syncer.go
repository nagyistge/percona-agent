package qh

import (
	"math"
	"time"
)

type IntervalSyncer struct {
}

func (s *IntervalSyncer) Sync(interval uint, n float64, sleep func(time.Duration)) *time.Ticker {
	// n := float64(t.UnixNano())
	i := float64(interval * time.Second)
	d := i - math.Mod(n, i)
	sleep(time.Duration(d) * time.Nanosecond)
	ticker := time.NewTicker(interval * time.Second)
	return ticker
}
