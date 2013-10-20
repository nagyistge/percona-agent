package mock

import (
	"github.com/percona/percona-cloud-tools/qa/interval"
)

type Iter struct {
	Chan chan *interval.Interval
}

func (i *Iter) IntervalChan() chan *interval.Interval {
	return i.Chan
}

func (i *Iter) Run() {
	return
}
