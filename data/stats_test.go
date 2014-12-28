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

package data_test

import (
	"fmt"
	. "github.com/percona/go-test/test"
	"github.com/percona/percona-agent/data"
	. "gopkg.in/check.v1"
	"time"
)

type StatsTestSuite struct {
	now  time.Time
	send []data.SentInfo
}

var _ = Suite(&StatsTestSuite{})

func (s *StatsTestSuite) SetUpSuite(t *C) {
	s.now = time.Now().UTC()

	// +1s results in 0.999999s diff, so +1.1s to workaround.
	s.send = []data.SentInfo{
		data.SentInfo{
			At:      s.now.Add(1100 * time.Millisecond),
			Seconds: 0.5,
			Files:   1,
			Bytes:   11100,
		},
		data.SentInfo{
			At:      s.now.Add(2100 * time.Millisecond),
			Seconds: 0.6,
			Files:   1,
			Bytes:   22200,
		},
		data.SentInfo{
			At:      s.now.Add(3100 * time.Millisecond),
			Seconds: 0.7,
			Files:   1,
			Bytes:   33300,
		},
		data.SentInfo{
			At:      s.now.Add(4100 * time.Millisecond),
			Seconds: 0.8,
			Files:   1,
			Bytes:   44400,
		},
		data.SentInfo{
			At:      s.now.Add(5100 * time.Millisecond),
			Seconds: 1.2, // todo: fix, can't be longer than interval between sends
			Files:   3,
			Bytes:   5155505,
		},
		data.SentInfo{
			At:      s.now.Add(6100 * time.Millisecond),
			Seconds: 1.0,
			Files:   2,
			Bytes:   606061,
		},
	}
}

// --------------------------------------------------------------------------

func (s *StatsTestSuite) TestRoundRobinFull(t *C) {
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 1)

	for _, info := range s.send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != 3 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 3", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[5]); !same {
		t.Error(diff)
	}

	if same, diff := IsDeeply(d[1], s.send[3]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[2], s.send[4]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[len(s.send)-4].At,
		End:         s.send[len(s.send)-1].At,
		Bytes:       "5.81 MB",
		Duration:    "3s",
		Utilization: "15.48 Mbps",
		Throughput:  "15.48 Mbps",
		Files:       6,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	t.Check(
		data.FormatSentReport(got),
		Equals,
		fmt.Sprintf(data.BaseReportFormat,
			expect.Files, expect.Bytes, expect.Duration, expect.Utilization, expect.Throughput),
	)
}

func (s *StatsTestSuite) TestRoundRobinPartial(t *C) {
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 1)

	for i := 0; i < 4; i++ {
		ss.Sent(s.send[i])
	}

	d = ss.Dump()
	if len(d) != 3 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 3", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[2]); !same {
		t.Error(diff)
	}

	if same, diff := IsDeeply(d[1], s.send[3]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[2], s.send[1]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[0].At,
		End:         s.send[3].At,
		Bytes:       "99.90 kB",
		Duration:    "3s",
		Utilization: "0.27 Mbps",
		Throughput:  "0.38 Mbps",
		Files:       3,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestOnlyLast(t *C) {
	ss := data.NewSenderStats(time.Duration(0))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 1)

	for i := 0; i < 4; i++ {
		ss.Sent(s.send[i])
	}

	d = ss.Dump()
	if len(d) != 1 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 1", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[3]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[2].At,
		End:         s.send[3].At,
		Bytes:       "44.40 kB",
		Duration:    "1s",
		Utilization: "0.36 Mbps",
		Throughput:  "0.44 Mbps",
		Files:       1,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestErrors(t *C) {
	ss := data.NewSenderStats(time.Duration(10 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 1)

	// Copy data so we can add errors.
	send := make([]data.SentInfo, len(s.send))
	for i, info := range s.send {
		send[i] = info
	}
	send[0].Errs++
	send[1].ApiErrs++
	send[2].BadFiles++
	send[3].Timeouts++
	for _, info := range send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != len(send)+1 {
		Dump(d)
		t.Errorf("len(d)=%d, expected %d", len(d), len(send)+1)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       d[0].At,
		End:         send[len(s.send)-1].At,
		Bytes:       "5.87 MB",
		Duration:    "6.1s",
		Utilization: "7.70 Mbps",
		Throughput:  "9.79 Mbps",
		Files:       9,
		Errs:        1,
		ApiErrs:     1,
		Timeouts:    1,
		BadFiles:    1,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	t.Check(
		data.FormatSentReport(got),
		Equals,
		fmt.Sprintf(data.BaseReportFormat+", "+data.ErrorReportFormat,
			expect.Files, expect.Bytes, expect.Duration, expect.Utilization, expect.Throughput,
			expect.Errs, expect.ApiErrs, expect.Timeouts, expect.BadFiles),
	)
}
