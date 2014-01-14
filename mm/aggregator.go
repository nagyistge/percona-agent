package mm

import (
	"github.com/percona/cloud-tools/pct"
	"time"
)

type Aggregator struct {
	ticker         pct.Ticker
	collectionChan chan *Collection
	dataChan       chan interface{}
	sync           *pct.SyncChan
	running        bool
}

func NewAggregator(ticker pct.Ticker, collectionChan chan *Collection, dataChan chan interface{}) *Aggregator {
	a := &Aggregator{
		ticker:         ticker,
		collectionChan: collectionChan,
		dataChan:       dataChan,
		sync:           pct.NewSyncChan(),
	}
	return a
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (a *Aggregator) Start() {
	a.ticker.Sync(time.Now().UnixNano())
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
		case now := <-a.ticker.TickerChan():
			// Even interval clock tick, e.g. 00:01:00.000, 00:02:00.000, etc.
			if !startTs.IsZero() {
				a.report(startTs, cur)
			}
			// Next interval starts now.
			startTs = now
			cur = make(map[string]*Stats)
		case collection := <-a.collectionChan:
			// todo: if colllect.Ts < lastNow, then discard: it missed its period
			for _, metric := range collection.Metrics {
				stats, haveStats := cur[metric.Name]
				if !haveStats {
					stats = NewStats(metric.Type)
					cur[metric.Name] = stats
				}
				stats.Add(&metric)
			}
		case <-a.sync.StopChan:
			return
		}
	}
}

// @goroutine[1]
func (a *Aggregator) report(startTs time.Time, stats map[string]*Stats) {
	for _, s := range stats {
		s.Summarize()
	}
	report := &Report{
		StartTs: startTs.UTC().Unix(),
		Metrics: stats,
	}
	select {
	case a.dataChan <- report:
	default:
		// todo: timeout instead
		// todo: lost report
	}
}
