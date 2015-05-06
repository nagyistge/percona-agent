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

package mm

import (
	"fmt"
	"math"
	"time"

	"github.com/percona/percona-agent/data"
	"github.com/percona/percona-agent/pct"
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
	go a.run()
	a.running = true // XXX: not guarded
}

// @goroutine[0]
func (a *Aggregator) Stop() {
	a.sync.Stop()
	a.sync.Wait()
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (a *Aggregator) run() {
	defer func() {
		if err := recover(); err != nil {
			a.logger.Error("Aggregator crashed: ", err)
		}
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
				a.logger.Debug("Start first interval", startTs)
			}
			if interval > curInterval {
				// Metrics for next interval have arrived.  Process and spool
				// the current interval, then advance to this interval.
				a.report(startTs, cur)

				// Init next stats based on current ones to avoid re-creating them.
				// todo: what if metrics from an instance aren't collected?
				for n := range cur {
					for key, _ := range cur[n].Stats {
						cur[n].Stats[key].Reset()
					}
				}
				curInterval = interval
				startTs = GoTime(a.interval, interval)
				a.logger.Debug("Start interval", startTs)
			} else if interval < curInterval {
				t := GoTime(a.interval, interval)
				a.logger.Info("Lost collection for interval", t, "; current interval is", startTs)
			}

			// Each collection is from a specific service instance.
			// Find the stats for this instance, create if they don't exist.
			var is *InstanceStats
			for _, i := range cur {
				if collection.UUID == i.UUID {
					is = i
					break
				}
			}

			if is == nil {
				// New service instance, create stats for it.
				is = &InstanceStats{
					UUID:  collection.UUID,
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
				if err := stats.Add(&metric, collection.Ts); err != nil {
					f := a.logger.Error
					switch err.(type) {
					case ErrValueLap:
						// Treat this error as info
						f = a.logger.Info
					}
					f(fmt.Sprintf("stats.Add(%+v, %d): %s", metric, collection.Ts, err))
				}
			}
		case <-a.sync.StopChan:
			return
		}
	}
}

// @goroutine[1]
func (a *Aggregator) report(startTs time.Time, is []*InstanceStats) {
	a.logger.Debug("Summarize metrics for", startTs)

	// The instance stats given (is) are a persistent buffer, so we need
	// to copy and filter the contents for the report, else the next interval
	// could change something which will affect values already reported because
	// we use pointers to all data structs.  Also, it's possible this
	// interval does not have metrics reported in previosu intervals
	// (i.e. no values, Cnt=0); see https://jira.percona.com/browse/PCT-911.
	finalInstanceStats := []*InstanceStats{}
	for _, i := range is {

		// Finalize the stats for every metric.  If the final stats are nil,
		// then no values were reported (Cnt=0), so we ignore the metric.
		finalMetrics := make(map[string]*Stats)
		for metric, stats := range i.Stats {
			finalStats := stats.Finalize()
			if finalStats == nil {
				// No values, so no stats; ignore the metric.
				continue
			}
			finalMetrics[metric] = finalStats
		}

		// If the instance has no metrics with stats; ignore it.  This can
		// happen if, for example, the MySQL metrics take too long to collect.
		// This isn't reported here; the metrics monitor should report it
		// because it knows that it collect any metrics.
		if len(finalMetrics) == 0 {
			continue
		}

		// Create a copy of this instance with the copy of its stats.
		finalInstance := &InstanceStats{
			UUID:  i.UUID,
			Stats: finalMetrics,
		}
		finalInstanceStats = append(finalInstanceStats, finalInstance)
	}

	if len(finalInstanceStats) == 0 {
		// This shouldn't happen: no instances with valid metrics/stats.
		a.logger.Warn("No metrics collected for", startTs)
		return
	}

	report := &Report{
		Ts:       startTs,
		Duration: uint(a.interval),
		Stats:    finalInstanceStats,
	}
	if err := a.spool.Write("mm", report); err != nil {
		a.logger.Warn("Lost report:", err)
	}
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
