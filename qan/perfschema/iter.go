/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

package perfschema

import (
	"time"

	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
)

type Iter struct {
	logger   *pct.Logger
	tickChan chan time.Time
	// --
	intervalChan chan *qan.Interval
	sync         *pct.SyncChan
}

func NewIter(logger *pct.Logger, tickChan chan time.Time) *Iter {
	iter := &Iter{
		logger:   logger,
		tickChan: tickChan,
		// --
		intervalChan: make(chan *qan.Interval, 1),
		sync:         pct.NewSyncChan(),
	}
	return iter
}

func (i *Iter) Start() {
	go i.run()
}

func (i *Iter) Stop() {
	i.sync.Stop()
	i.sync.Wait()
	return
}

func (i *Iter) IntervalChan() chan *qan.Interval {
	return i.intervalChan
}

func (i *Iter) TickChan() chan time.Time {
	return i.tickChan
}

// --------------------------------------------------------------------------

func (i *Iter) run() {
	defer func() {
		if err := recover(); err != nil {
			i.logger.Error("QAN performance schema iterator crashed: ", err)
		}
		i.sync.Done()
	}()

	prev := time.Time{}
	cur := &qan.Interval{
		StartTime: prev,
	}
	for {
		i.logger.Debug("run:wait")
		select {
		case now := <-i.tickChan:
			i.logger.Debug("run:tick")
			cur.Number++
			cur.StartTime = prev
			cur.StopTime = now
			prev = now
			select {
			case i.intervalChan <- cur:
			case <-time.After(1 * time.Second):
				i.logger.Warn("Lost interval: ", cur)
			}
		case <-i.sync.StopChan:
			i.logger.Debug("run:stop")
			return
		}
	}
}
