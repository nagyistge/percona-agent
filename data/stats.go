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
	"fmt"
	"github.com/percona/percona-agent/pct"
	"time"
)

var DebugStats = false

type SentInfo struct {
	Begin    time.Time
	End      time.Time
	SendTime float64
	Files    uint
	Bytes    int
	Errs     uint
	ApiErrs  uint
	Timeouts uint
	BadFiles uint
}

type SentReport struct {
	bytes    int
	sendTime float64
	// --
	Begin       time.Time
	End         time.Time
	Bytes       string // humanized bytes, e.g. 443.59 kB
	Duration    string // End - Begin, humanized
	Utilization string // bytes / (End - Begin), Mbps
	Throughput  string // bytes / sendTime, Mbps
	Files       uint
	Errs        uint
	ApiErrs     uint
	Timeouts    uint
	BadFiles    uint
}

var (
	BaseReportFormat  string = "%d files, %s, %s, %s net util, %s net speed"
	ErrorReportFormat        = "%d errors, %d API errors, %d timeouts, %d bad files"
)

type SenderStats struct {
	d time.Duration
	// --
	begin time.Time  // oldest SentInfo.Begin in sent
	end   time.Time  // latest SentInfo.End in sent
	sent  []SentInfo // round-robin
	full  bool       // sent is at max size
	i     int        // index into sent once full
}

func NewSenderStats(d time.Duration) *SenderStats {
	s := &SenderStats{
		d: d,
		// --
		sent: []SentInfo{},
	}
	return s
}

func (s *SenderStats) Sent(info SentInfo) {
	if DebugStats {
		fmt.Printf("\n%+v\n", info)
		fmt.Println(s.sent)
	}

	if s.begin.IsZero() {
		s.begin = info.Begin.UTC()
	}
	s.end = info.End.UTC()

	if DebugStats {
		fmt.Printf("range: %s to %s (%s)\n", pct.TimeString(s.begin), pct.TimeString(s.end), s.end.Sub(s.begin))
		defer func() {
			fmt.Printf("range: %s to %s (%s)\n", pct.TimeString(s.begin), pct.TimeString(s.end), s.end.Sub(s.begin))
		}()
	}

	if !s.full {
		if info.End.UTC().Sub(s.begin) < s.d {
			// Full duration hasn't elapsed yet. Append this info and keep
			// growing the buffer.
		} else {
			// Full duration has elapsed. Store this info on the end of the
			// buffer, then cycle through the array, overwriting older info.
			if DebugStats {
				fmt.Println("full")
			}
			s.full = true
			s.i = 0 // next info overwrites oldest info
		}
		s.sent = append(s.sent, info)
		if DebugStats {
			fmt.Println("store at", len(s.sent)-1, "(append)")
		}
		return
	}

	// We're going to overwite the oldest begin. If the next newest begin - end
	// is < duration then we need to re-grow the buffer. This happens when info
	// is reported slowly at first (e.g. only 2 info to elapse the duration) then
	// more rapidly (e.g. 6 info to elapse the duration). This re-grows the buffer
	// at the faster rate, i.e. more info per duration.
	nextBegin := s.i + 1
	if nextBegin == len(s.sent) {
		nextBegin = 0
	}
	if s.end.Sub(s.sent[nextBegin].Begin.UTC()) < s.d {
		if DebugStats {
			fmt.Println("re-grow buffer")
		}
		s.full = false
		newSent := []SentInfo{}
		for _, oldInfo := range s.sent {
			if oldInfo.Begin.Before(s.begin) {
				continue
			}
			newSent = append(newSent, oldInfo)
		}
		newSent = append(newSent, info)
		s.sent = newSent
		s.i = len(s.sent)
		s.sent[0].Begin.UTC()
		s.sent[len(s.sent)-1].End.UTC()
		return
	}

	// Find the shortest interval where end - begin >= duration.
	// This helps avoid intervals that are too big due to gaps
	// and inconsistent reporting.
	if s.d > 0 {
		j := s.i // oldest begin

		// For however many items we have...
		for n := 0; n < len(s.sent); n++ {

			// Check if end - begin < duration is too short.
			d := s.end.Sub(s.sent[j].Begin.UTC())
			if DebugStats {
				fmt.Println("j=", j, d)
			}
			if d < s.d {
				break // duration too short
			}

			// Duration is long enough; try next newest begin.
			j++
			if j == len(s.sent) {
				j = 0 // wrap to start
			}
		}

		// j is the first begin that makes the interval too short,
		// so back up one.
		if j == 0 {
			j = len(s.sent) - 1
		} else {
			j--
		}

		if DebugStats {
			fmt.Println("begin at", j, s.sent[j].Begin.UTC())
		}
		s.begin = s.sent[j].Begin.UTC()
	} else {
		// Only saving last report. Can't shorten it.
		s.begin = info.Begin.UTC()
	}

	// Save the current info.
	if DebugStats {
		fmt.Println("store at", s.i, "(overwrite)")
	}
	s.sent[s.i] = info

	// Advance the index. Wrap to start if needed.
	s.i++
	if s.i == len(s.sent) {
		s.i = 0
	}
}

func (s *SenderStats) Report() SentReport {
	r := SentReport{
		Begin: s.begin,
		End:   s.end,
	}
	for _, info := range s.sent {
		if info.Begin.Before(s.begin) {
			if DebugStats {
				fmt.Println("skip", info)
			}
			continue
		}
		r.bytes += info.Bytes
		r.sendTime += info.SendTime

		r.Files += info.Files
		r.Errs += info.Errs
		r.ApiErrs += info.ApiErrs
		r.Timeouts += info.Timeouts
		r.BadFiles += info.BadFiles
	}
	r.Bytes = pct.Bytes(r.bytes)
	r.Duration = pct.Duration(s.end.Sub(s.begin).Seconds())
	r.Utilization = pct.Mbps(r.bytes, s.end.Sub(s.begin).Seconds()) + " Mbps"
	r.Throughput = pct.Mbps(r.bytes, r.sendTime) + " Mbps"
	return r
}

func FormatSentReport(r SentReport) string {
	report := fmt.Sprintf(BaseReportFormat, r.Files, r.Bytes, r.Duration, r.Utilization, r.Throughput)
	if (r.Errs + r.BadFiles + r.ApiErrs + r.Timeouts) > 0 {
		report += ", " + fmt.Sprintf(ErrorReportFormat, r.Errs, r.ApiErrs, r.Timeouts, r.BadFiles)
	}
	return report
}

func (s *SenderStats) Dump() []SentInfo {
	return s.sent
}
