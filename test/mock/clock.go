package mock

import (
	"time"
)

type Clock struct {
	Added   []uint
	Removed []chan time.Time
	Eta     float64
}

func NewClock() *Clock {
	m := &Clock{
		Added:   []uint{},
		Removed: []chan time.Time{},
	}
	return m
}

func (m *Clock) Add(c chan time.Time, t uint, sync bool) {
	m.Added = append(m.Added, t)
}

func (m *Clock) Remove(c chan time.Time) {
	m.Removed = append(m.Removed, c)
}

func (m *Clock) ETA(c chan time.Time) float64 {
	return m.Eta
}
