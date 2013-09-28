package interval

import (
	"time"
)

type Interval struct {
	Filename string
	StartTime time.Time
	StopTime time.Time
	StartOffset int64
	StopOffset int64
}
