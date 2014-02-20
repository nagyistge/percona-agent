/*
    Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package ticker

import (
	"sync"
	"time"
)

type Manager interface {
	Add(c chan time.Time, atInterval uint)
	Remove(c chan time.Time)
}

// Solid gold
type Rolex struct {
	tickerFactory TickerFactory
	ticker        map[uint]Ticker
	tickerMux     *sync.Mutex
	nowFunc       func() int64
	watcher      map[chan time.Time]Ticker
	watcherMux   *sync.Mutex
}

func NewRolex(tickerFactory TickerFactory, nowFunc func() int64) *Rolex {
	r := &Rolex{
		tickerFactory: tickerFactory,
		nowFunc:       nowFunc,
		ticker:        make(map[uint]Ticker),
		tickerMux:     new(sync.Mutex),
		watcher: make(map[chan time.Time]Ticker),
		watcherMux:     new(sync.Mutex),
	}
	return r
}

func (r *Rolex) Add(c chan time.Time, atInterval uint) {
	r.tickerMux.Lock()
	defer r.tickerMux.Unlock()
	ticker, ok := r.ticker[atInterval]
	if !ok {
		ticker = r.tickerFactory.Make(atInterval)
		go ticker.Run(r.nowFunc())
		r.ticker[atInterval] = ticker
	}
	ticker.Add(c)

	r.watcherMux.Lock()
	defer r.watcherMux.Unlock()
	r.watcher[c] = ticker

	// todo: stop ticker if it has no watchers
}

func (r *Rolex) Remove(c chan time.Time) {
	r.watcherMux.Lock()
	defer r.watcherMux.Unlock()
	if ticker, ok := r.watcher[c]; ok {
		ticker.Remove(c)
		delete(r.watcher, c)
	}
}
