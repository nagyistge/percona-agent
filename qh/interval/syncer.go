package interval

import (
	"math"
	"time"
)

type Syncer struct {
	atInterval uint
	sleep func(time.Duration)
}

func NewSyncer(atInterval uint, sleep func(time.Duration)) *Syncer {
	s := &Syncer{
		atInterval: atInterval,
		sleep: sleep,
	}
	return s
}

func (s *Syncer) Sync(nowNanosecond int64) *time.Ticker {
	i := float64(time.Duration(s.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	s.sleep(time.Duration(d) * time.Nanosecond)
	ticker := time.NewTicker(time.Duration(s.atInterval) * time.Second)
	return ticker
}
