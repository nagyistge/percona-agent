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
	cur := make(Metrics)
	begin := time.Now().UTC()

	for {
		select {
		case now := <-a.tickChan:
			// Even clock tick, e.g. 00:01:00.000, 00:02:00.000, etc.
			a.report(begin, now, cur)

			// Next interval starts now.
			begin = now
			cur = make(Metrics)
		case collection := <-a.collectionChan:
			// todo: if colllect.Ts < begin, then discard: it missed its period
			for _, metric := range collection.Metrics {
				stats, haveStats := cur[metric.Name]
				if !haveStats {
					stats = NewStats(metric.Type)
					cur[metric.Name] = stats
				}
				stats.Add(&metric, collection.StartTs)
			}
		case <-a.sync.StopChan:
			return
		}
	}
}

// @goroutine[1]
func (a *Aggregator) report(begin, end time.Time, metrics Metrics) {
	d := end.Sub(begin).Seconds()
	a.logger.Info("Summarize metrics from", begin, "to", end, d)
	for _, s := range metrics {
		s.Summarize()
	}
	report := &Report{
		Duration: uint(d),
		Ts:       end,
		Metrics:  metrics,
	}
	a.spool.Write("mm", report)
}
