package mock

import (
	"github.com/percona/cloud-tools/pct"
)

type TickerFactory struct {
	tickers  []pct.Ticker
	tickerNo int
	Made     []uint
}

func NewTickerFactory() *TickerFactory {
	tf := &TickerFactory{
		Made: []uint{},
	}
	return tf
}

func (tf *TickerFactory) Make(atInterval uint) pct.Ticker {
	tf.Made = append(tf.Made, atInterval)
	if tf.tickerNo > len(tf.tickers) {
		return tf.tickers[tf.tickerNo-1]
	}
	nextTicker := tf.tickers[tf.tickerNo]
	tf.tickerNo++
	return nextTicker
}

func (tf *TickerFactory) Set(tickers []pct.Ticker) {
	tf.tickerNo = 0
	tf.tickers = tickers
	tf.Made = []uint{}
}
