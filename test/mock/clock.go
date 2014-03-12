package mock

import (
	"time"
)

type Clock struct {
	Added   []uint
	Removed []chan time.Time
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
