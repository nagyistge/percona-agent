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
	"github.com/percona/cloud-tools/instance"
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
	cur := []*InstanceStats{}

	for {
		select {
		case now := <-a.tickChan:
			// Even clock tick, e.g. 00:01:00.000, 00:02:00.000, etc.
			if !startTs.IsZero() {
				a.report(startTs, now, cur)
			}
			// Next interval starts now.
			startTs = now

			// Init next stats based on current ones to avoid re-creating them.
			// todo: what if metrics from an instance aren't collected?
			next := make([]*InstanceStats, len(cur))
			for n := range cur {
				i := &InstanceStats{
					Config: cur[n].Config,
					Stats:  make(map[string]*Stats),
				}
				next[n] = i
			}
			cur = next

			a.logger.Debug("Start report interval")
		case collection := <-a.collectionChan:
			// todo: if colllect.Ts < lastNow, then discard: it missed its period

			// Each collection is from a specific service instance ("it").
			// Find the stats for this instance, create if they don't exist.
			var is *InstanceStats
			for _, i := range cur {
				if collection.Service == i.Service && collection.InstanceId == i.InstanceId {
					is = i
					break
				}
			}

			if is == nil {
				// New service instance, create stats for it.
				is = &InstanceStats{
					Config: instance.Config{
						Service:    collection.Service,
						InstanceId: collection.InstanceId,
					},
					Stats: make(map[string]*Stats),
				}
				cur = append(cur, is)
			}

			// Add each metric in the collection to its Stats.
			for _, metric := range collection.Metrics {
				stats, haveStats := is.Stats[metric.Name]
				if !haveStats {
					// New metric, create stats for it.
					var err error
					stats, err = NewStats(metric.Type)
					if err != nil {
						a.logger.Error(metric.Name, "invalid:", err.Error())
						continue
					}
					is.Stats[metric.Name] = stats
				}
				stats.Add(&metric, collection.Ts)
			}
		case <-a.sync.StopChan:
			return
		}
	}
}

// @goroutine[1]
func (a *Aggregator) report(startTs, endTs time.Time, is []*InstanceStats) {
	d := uint(endTs.Unix() - startTs.Unix())
	a.logger.Info("Summarize metrics from", startTs, "to", endTs, "in", d)
	for _, i := range is {
		for _, s := range i.Stats {
			s.Summarize()
		}
	}
	report := &Report{
		Ts:       startTs,
		Duration: d,
		Stats:    is,
	}
	a.spool.Write("mm", report)
}
