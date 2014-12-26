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

package data

import (
	"github.com/percona/percona-agent/pct"
	"time"
)

type SentInfo struct {
	At       time.Time
	Seconds  float64
	Files    uint
	Bytes    int
	Errs     uint
	ApiErrs  uint
	Timeouts uint
	BadFiles uint
}

type SentReport struct {
	bytes   int
	seconds float64
	// --
	LastSent time.Time
	Time     string
	Bytes    string
	Mbps     string
	Files    uint
	Errs     uint
	ApiErrs  uint
	Timeouts uint
	BadFiles uint
}

type SenderStats struct {
	d time.Duration
	// --
	last   time.Time  // last SentInfo.At sent
	oldest time.Time  // oldest SentInfo.At in sent
	sent   []SentInfo // round-robin
	full   bool       // sent is at max size
	i      int        // index into sent once full
}

func NewSenderStats(d time.Duration) *SenderStats {
	sent := []SentInfo{
		SentInfo{At: time.Now().UTC()},
	}
	s := &SenderStats{
		d: d,
		// --
		sent:   sent,
		oldest: sent[0].At.UTC(),
	}
	return s
}

func (s *SenderStats) Sent(info SentInfo) {
	s.last = info.At.UTC()

	if !s.full {
		if info.At.UTC().Sub(s.oldest) < s.d {
			// Full duration hasn't elapsed yet. Keep growing the rrd.
			s.sent = append(s.sent, info)
			s.i++
		} else {
			// Full duration has elapsed. Recurse once to store this info at
			// sent[0], then keep cycling through the fix-sized array.
			s.full = true
			s.i = 0
			s.Sent(info)
		}
		return
	}

	// Store info at next element in sent. Start over at sent[0]
	// when we reach the end.
	s.oldest = s.sent[s.i].At.UTC()
	s.sent[s.i] = info
	s.i++
	if s.i == len(s.sent) {
		s.i = 0
	}
}

func (s *SenderStats) Report() SentReport {
	r := SentReport{
		LastSent: s.last,
	}
	for _, info := range s.sent {
		r.bytes += info.Bytes
		r.seconds += info.Seconds

		r.Files += info.Files
		r.Errs += info.Errs
		r.ApiErrs += info.ApiErrs
		r.Timeouts += info.Timeouts
		r.BadFiles += info.BadFiles
	}
	r.Bytes = pct.Bytes(r.bytes)
	r.Mbps = pct.Mbps(r.bytes, r.seconds)
	r.Time = time.Duration(r.seconds * float64(time.Second)).String()
	return r
}

func (s *SenderStats) Dump() []SentInfo {
	return s.sent
}
