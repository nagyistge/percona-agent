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
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/data"
	"github.com/percona/cloud-tools/pct"
	"math"
	"time"
)

type Aggregator struct {
	logger         *pct.Logger
	interval       int64
	collectionChan chan *Collection
	spool          data.Spooler
	// --
	sync    *pct.SyncChan
	running bool
}

func NewAggregator(logger *pct.Logger, interval int64, collectionChan chan *Collection, spool data.Spooler) *Aggregator {
	a := &Aggregator{
		logger:         logger,
		interval:       interval,
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

	var curInterval int64
	var startTs time.Time
	cur := []*InstanceStats{}

	for {
		select {
		case collection := <-a.collectionChan:
			interval := (collection.Ts / a.interval) * a.interval
			if curInterval == 0 {
				curInterval = interval
				startTs = GoTime(a.interval, interval)
			}
			if interval > curInterval {
				// Metrics for next interval have arrived.  Process and spool
				// the current interval, then advance to this interval.
				a.report(startTs, cur)

				// Init next stats based on current ones to avoid re-creating them.
				// todo: what if metrics from an instance aren't collected?
				next := make([]*InstanceStats, len(cur))
				for n := range cur {
					i := &InstanceStats{
						ServiceInstance: cur[n].ServiceInstance,
						Stats:           make(map[string]*Stats),
					}
					next[n] = i
				}
				cur = next
				curInterval = interval
				startTs = GoTime(a.interval, interval)
			} else if interval < curInterval {
				// collection arrived late
			}

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
					ServiceInstance: proto.ServiceInstance{
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
func (a *Aggregator) report(startTs time.Time, is []*InstanceStats) {
	a.logger.Info("Summarize metrics for", startTs)
	for _, i := range is {
		for _, s := range i.Stats {
			s.Summarize()
		}
	}
	report := &Report{
		Ts:       startTs,
		Duration: uint(a.interval),
		Stats:    is,
	}
	a.spool.Write("mm", report)
}

func GoTime(interval, unixTs int64) time.Time {
	// Calculate seconds (d) from begin to next interval.
	i := float64(interval)
	t := float64(unixTs)
	d := int64(i - math.Mod(t, i))
	if d != interval {
		/**
		 * unixTs is not an interval, so it's after the interval's start ts.
		 * E.g. if i=60 and unxiTs (t)=130, then t falls between intervals:
		 *   120
		 *   130  =t
		 *   180  d=50
		 * Real begin is 120, so decrease t by 10: i - d.
		 */
		unixTs = unixTs - (interval - d)
	}
	return time.Unix(int64(unixTs), 0).UTC()
}
