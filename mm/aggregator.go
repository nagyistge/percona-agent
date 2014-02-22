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

package mm

import (
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/pct"
	"time"
)

type Aggregator struct {
	logger         *pct.Logger
	tickChan       chan time.Time
	collectionChan chan *Collection
	spool          data.Spooler
	// --
	sync    *pct.SyncChan
	running bool
}

func NewAggregator(logger *pct.Logger, tickChan chan time.Time, collectionChan chan *Collection, spool data.Spooler) *Aggregator {
	a := &Aggregator{
		logger:         logger,
		tickChan:       tickChan,
		collectionChan: collectionChan,
		spool:          spool,
		// --
		sync: pct.NewSyncChan(),
	}
	return a
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (a *Aggregator) Start() {
	a.running = true // XXX: not guarded
	go a.run()
}

// @goroutine[0]
func (a *Aggregator) Stop() {
	a.sync.Stop()
	a.sync.Wait()
}

// @goroutine[0]
func (a *Aggregator) IsRunning() bool {
	return a.running // XXX: not guarded
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (a *Aggregator) run() {
	defer func() {
		a.running = false // XXX: not guarded
		a.sync.Done()
	}()

	/**
	 * We aggregate on clock ticks.  The first tick becomes the first
	 * interval's start ts.  Before that, we receive but ultimately throw
	 * away any metrics.  This is ok because we shouldn't wait long for
	 * the first tick, and it decouples starting/running monitors and
	 * aggregators, i.e. neither should have to wait for the other.
	 */
	var startTs time.Time
	cur := make(Metrics)

	for {
		select {
		case now := <-a.tickChan:
			// Even clock tick, e.g. 00:01:00.000, 00:02:00.000, etc.
			if !startTs.IsZero() {
				a.report(startTs, now, cur)
			}
			// Next interval starts now.
			startTs = now
			cur = make(Metrics)
			a.logger.Debug("Start report interval")
		case collection := <-a.collectionChan:
			// todo: if colllect.Ts < lastNow, then discard: it missed its period
			for _, metric := range collection.Metrics {
				stats, haveStats := cur[metric.Name]
				if !haveStats {
					stats = NewStats(metric.Type)
					cur[metric.Name] = stats
				}
				stats.Add(&metric, collection.Ts)
			}
		case <-a.sync.StopChan:
			return
		}
	}
}

// @goroutine[1]
func (a *Aggregator) report(startTs, endTs time.Time, metrics Metrics) {
	d := uint(endTs.Unix() - startTs.Unix())
	a.logger.Info("Summarize metrics from", startTs, "to", endTs, "in", d)
	for _, s := range metrics {
		s.Summarize()
	}
	report := &Report{
		Ts:       startTs,
		Duration: d,
		Metrics:  metrics,
	}
	a.spool.Write("mm", report)
}
