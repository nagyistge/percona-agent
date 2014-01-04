package mm

import (
	"sort"
)

type Stats struct {
	vals  []float64 `json:"-"` // ignore
	sum   float64   `json:"-"` // ignore
	Cnt   int
	Min   float64
	Pct5  float64
	Avg   float64
	Med   float64
	Pct95 float64
	Max   float64
}

func NewStats() *Stats {
	s := &Stats{
		vals: []float64{},
	}
	return s
}

func (s *Stats) Add(val float64) {
	s.vals = append(s.vals, val)
	s.sum += val
}

func (s *Stats) Summarize() {
	s.Cnt = len(s.vals)
	if s.Cnt > 1 {
		sort.Float64s(s.vals)

		s.Min = s.vals[0]
		s.Pct5 = s.vals[(5*s.Cnt)/100]
		s.Avg = s.sum / float64(s.Cnt)
		s.Med = s.vals[(50*s.Cnt)/100] // median = 50th percentile
		s.Pct95 = s.vals[(95*s.Cnt)/100]
		s.Max = s.vals[s.Cnt-1]
	} else if s.Cnt == 1 {
		s.Min = s.vals[0]
		s.Pct5 = s.vals[0]
		s.Avg = s.vals[0]
		s.Med = s.vals[0]
		s.Pct95 = s.vals[0]
		s.Max = s.vals[0]
	} else {
		// no values, all zeros
	}
}
