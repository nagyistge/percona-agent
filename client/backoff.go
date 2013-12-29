package client

import (
	"time"
	"math"
	"math/rand"
)

type Backoff struct {
	try int
	lastSuccess time.Time
}

func NewBackoff() *Backoff {
	b := new(Backoff)
	return b
}

func (b *Backoff) Wait() time.Duration {
	var t int
	if b.try == 0 {
		t = 0
		b.try++
	} else if b.try < 7 {
		// 1s, 3s, 7s, 15s, 31s, 1m3s = 2m
		t = int(math.Pow(2, float64(b.try)) - 1)
		b.try++
	} else {
		// [1m30s, 3m)
		t = int(90 + (90 * rand.Float64()))
	}
	return time.Duration(t) * time.Second
}

func (b *Backoff) Success() {
	if b.lastSuccess.IsZero() {
		// First success, don't reset backoff yet because if the remote end
		// is flapping, there maybe be other tries real soon, so we want the
		// backoff wait to take effect.
		b.lastSuccess = time.Now()
	} else if b.lastSuccess.Sub(time.Now()) > 5 * time.Minute {
		// If it's been > 5m since the last success and this success,
		// then the remote end was flapping at least stopped for 5 minutes,
		// so we reset the backoff.
		b.lastSuccess = time.Now()
		b.try = 0
	}
}
