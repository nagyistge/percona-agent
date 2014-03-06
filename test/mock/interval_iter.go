package mock

import (
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	"time"
)

type IntervalIterFactory struct {
	Iters  []qan.IntervalIter
	iterNo int
}

func (tf *IntervalIterFactory) Make(filename qan.FilenameFunc, tickChan chan time.Time) qan.IntervalIter {
	if tf.iterNo >= len(tf.Iters) {
		return tf.Iters[tf.iterNo-1]
	}
	nextIter := tf.Iters[tf.iterNo]
	tf.iterNo++
	return nextIter
}

// --------------------------------------------------------------------------

type MockIntervalIter struct {
	testIntervalChan chan *qan.Interval
	intervalChan     chan *qan.Interval
	sync             *pct.SyncChan
}

func NewMockIntervalIter(intervalChan chan *qan.Interval) *MockIntervalIter {
	iter := &MockIntervalIter{
		intervalChan:     make(chan *qan.Interval),
		testIntervalChan: intervalChan,
		sync:             pct.NewSyncChan(),
	}
	return iter
}

func (i *MockIntervalIter) Start() {
	go i.run()
	return
}

func (i *MockIntervalIter) Stop() {
	i.sync.Stop()
	i.sync.Wait()
}

func (i *MockIntervalIter) IntervalChan() chan *qan.Interval {
	return i.intervalChan
}

func (i *MockIntervalIter) run() {
	defer func() {
		close(i.intervalChan)
		i.sync.Done()
	}()
	for {
		select {
		case <-i.sync.StopChan:
			return
		case interval := <-i.testIntervalChan:
			i.intervalChan <- interval
		}
	}
}
