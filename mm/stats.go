/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package mm

import (
	"sort"
)

type Stats struct {
	metricType byte      `json:"-"` // ignore
	str        string    `json:",omitempty"`
	firstVal   bool      `json:"-"`
	prevTs     int64     `json:"-"`
	prevVal    float64   `json:"-"`
	vals       []float64 `json:"-"`
	sum        float64   `json:"-"`
	Cnt        int
	Min        float64
	Pct5       float64
	Avg        float64
	Med        float64
	Pct95      float64
	Max        float64
}

func NewStats(metricType byte) *Stats {
	s := &Stats{
		metricType: metricType,
		vals:       []float64{},
		firstVal:   true,
	}
	return s
}

func (s *Stats) Add(m *Metric, ts int64) {
	switch s.metricType {
	case NUMBER:
		s.vals = append(s.vals, m.Number)
		s.sum += m.Number
	case COUNTER:
		if !s.firstVal {
			if m.Number >= s.prevVal {
				// Metric value increased (or stayed same); this is what we expect.

				// Per-second rate of value = increase / duration
				inc := m.Number - s.prevVal
				dur := ts - s.prevTs
				val := inc / float64(dur)
				s.vals = append(s.vals, val)

				// Keep running total to calc Avg.
				s.sum += inc

				// Current values become previous values.
				s.prevTs = ts
				s.prevVal = m.Number
			} else {
				// Metric value reset, e.g. FLUSH GLOBAL STATUS.
				s.prevTs = ts
				s.prevVal = m.Number
			}
		} else {
			s.prevTs = ts
			s.prevVal = m.Number
			s.firstVal = false
		}
	case STRING:
		if s.str == "" {
			s.str = m.String
		}
	}
}

func (s *Stats) Summarize() {
	switch s.metricType {
	case NUMBER, COUNTER:
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
		}
	}
}
