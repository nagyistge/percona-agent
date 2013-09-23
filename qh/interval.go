package qh

type Interval struct {
	SlowLogFile string
	StartTime time.Time
	StopTime time.Time
	StartOffset uint64
	StopOffset uint64
}
