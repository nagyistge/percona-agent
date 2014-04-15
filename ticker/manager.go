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
	"log"
	"sync"
	"time"
)

type Manager interface {
	Add(c chan time.Time, atInterval uint, sync bool)
	Remove(c chan time.Time)
	ETA(c chan time.Time) float64
}

type Clock struct {
	tickerFactory TickerFactory
	syncTicker    map[uint]Ticker
	syncTickerMux *sync.Mutex
	waitTicker    map[chan time.Time]Ticker
	waitTickerMux *sync.Mutex
	nowFunc       func() int64
	watcher       map[chan time.Time]Ticker
	watcherMux    *sync.Mutex
}

func NewClock(tickerFactory TickerFactory, nowFunc func() int64) *Clock {
	r := &Clock{
		tickerFactory: tickerFactory,
		nowFunc:       nowFunc,
		syncTicker:    make(map[uint]Ticker),
		syncTickerMux: new(sync.Mutex),
		waitTicker:    make(map[chan time.Time]Ticker),
		waitTickerMux: new(sync.Mutex),
		watcher:       make(map[chan time.Time]Ticker),
		watcherMux:    new(sync.Mutex),
	}
	return r
}

func (clock *Clock) Add(c chan time.Time, atInterval uint, sync bool) {
	var ticker Ticker
	var ok bool
	if sync {
		clock.syncTickerMux.Lock()
		defer clock.syncTickerMux.Unlock()
		ticker, ok = clock.syncTicker[atInterval]
		if !ok {
			ticker = clock.tickerFactory.Make(atInterval, sync)
			go ticker.Run(clock.nowFunc())
			clock.syncTicker[atInterval] = ticker
		}
	} else {
		clock.waitTickerMux.Lock()
		defer clock.waitTickerMux.Unlock()
		if _, ok := clock.waitTicker[c]; ok {
			log.Panic("WaitTicker exists for ", c)
		}
		ticker = clock.tickerFactory.Make(atInterval, sync)
		go ticker.Run(clock.nowFunc())
		clock.waitTicker[c] = ticker
	}

	ticker.Add(c)

	clock.watcherMux.Lock()
	defer clock.watcherMux.Unlock()
	clock.watcher[c] = ticker
}

func (clock *Clock) Remove(c chan time.Time) {
	clock.watcherMux.Lock()
	defer clock.watcherMux.Unlock()
	if ticker, ok := clock.watcher[c]; ok {
		ticker.Remove(c)
		delete(clock.watcher, c)
	}

	// todo: stop ticker if it has no watchers

	clock.waitTickerMux.Lock()
	defer clock.waitTickerMux.Unlock()
	if _, ok := clock.waitTicker[c]; ok {
		delete(clock.waitTicker, c)
	}
}

func (clock *Clock) ETA(c chan time.Time) float64 {
	clock.watcherMux.Lock()
	defer clock.watcherMux.Unlock()
	ticker, ok := clock.watcher[c]
	if !ok {
		return 0
	}
	return ticker.ETA(clock.nowFunc())
}
