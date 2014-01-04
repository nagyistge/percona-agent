package mm

import (
	pct "github.com/percona/cloud-tools"
	"time"
)

type Aggregator struct {
	tickerChan     chan time.Time
	collectionChan chan *Collection
	reportChan     chan *Report
	Sync           *pct.SyncChan
}

func NewAggregator(tickerChan chan time.Time, collectionChan chan *Collection, reportChan chan *Report) *Aggregator {
	a := &Aggregator{
		tickerChan:     tickerChan,
		collectionChan: collectionChan,
		reportChan:     reportChan,
		Sync:           pct.NewSyncChan(),
	}
	return a
}

func (a *Aggregator) Run() {
	defer a.Sync.Done()

	/**
	 * We aggregate on even intervals, from clock tick to clock tick.
	 * The first clock tick becomes the first interval's start ts;
	 * before that, we receive but ultimately throw away any metrics.
	 * This is ok because we shouldn't wait long for the first clock tick,
	 * and it decouples starting/running monitors and aggregators, i.e.
	 * neither should have to wait on or sync with the other.
	 */
	var startTs time.Time
	cur := make(map[string]*Stats)

	for {
		select {
		case now := <-a.tickerChan:
			// Even interval clock tick, e.g. 00:01:00.000, 00:02:00.000, etc.
			if !startTs.IsZero() {
				a.report(startTs, cur)
			}
			// Next interval starts now.
			startTs = now
			cur = make(map[string]*Stats)
		case collection := <-a.collectionChan:
			for _, metric := range collection.Metrics {
				stats, haveStats := cur[metric.Name]
				if haveStats {
					stats.Add(metric.Value)
				} else {
					stats = NewStats()
					stats.Add(metric.Value)
					cur[metric.Name] = stats
				}
			}
		case <-a.Sync.StopChan:
			return
		}
	}
}

func (a *Aggregator) report(startTs time.Time, stats map[string]*Stats) {
	for _, s := range stats {
		s.Summarize()
	}
	report := &Report{
		StartTs: startTs.UTC().Unix(),
		Metrics: stats,
	}
	select {
	case a.reportChan <- report:
	default:
		// todo: lost report
	}
}
