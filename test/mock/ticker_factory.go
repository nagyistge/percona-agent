package mock

import (
	"github.com/percona/cloud-tools/ticker"
)

type TickerFactory struct {
	tickers  []ticker.Ticker
	tickerNo int
	Made     []uint
}

func NewTickerFactory() *TickerFactory {
	tf := &TickerFactory{
		Made: []uint{},
	}
	return tf
}

func (tf *TickerFactory) Make(atInterval uint, sync bool) ticker.Ticker {
	tf.Made = append(tf.Made, atInterval)
	if tf.tickerNo > len(tf.tickers) {
		return tf.tickers[tf.tickerNo-1]
	}
	nextTicker := tf.tickers[tf.tickerNo]
	tf.tickerNo++
	return nextTicker
}

func (tf *TickerFactory) Set(tickers []ticker.Ticker) {
	tf.tickerNo = 0
	tf.tickers = tickers
	tf.Made = []uint{}
}
