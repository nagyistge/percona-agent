/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

package qan

import (
	"sort"
	"time"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/go-mysql/event"
	"github.com/percona/percona-agent/pct"
)

// slowlog|perf schema --> Result --> proto.QANReport --> data.Spooler

// Data for an interval from slow log or performance schema (pfs) parser,
// passed to MakeReport() which transforms into a proto.QANReport{}.
type Result struct {
	Global     *event.GlobalClass  // metrics for all data
	Class      []*event.QueryClass // per-class metrics
	RunTime    float64             // seconds parsing data, hopefully < interval
	StopOffset int64               // slow log offset where parsing stopped, should be <= end offset
	Error      string              `json:",omitempty"`
}

type ByQueryTime []*event.QueryClass

func (a ByQueryTime) Len() int      { return len(a) }
func (a ByQueryTime) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByQueryTime) Less(i, j int) bool {
	// todo: will panic if struct is incorrect
	// descending order
	return a[i].Metrics.TimeMetrics["Query_time"].Sum > a[j].Metrics.TimeMetrics["Query_time"].Sum
}

func MakeReport(config Config, interval *Interval, result *Result) *proto.QANReport {
	// Sort classes by Query_time_sum, descending.
	sort.Sort(ByQueryTime(result.Class))

	// Make proto.QANReport from Result and other metadata (e.g. Interval).
	report := &proto.QANReport{
		UUID:    config.UUID,
		StartTs: interval.StartTime,
		EndTs:   interval.StopTime,
		RunTime: result.RunTime,
		Global:  result.Global,
		Class:   result.Class,
	}
	if interval != nil {
		size, err := pct.FileSize(interval.Filename)
		if err != nil {
			size = 0
		}

		// slow log data
		report.SlowLogFile = interval.Filename
		report.SlowLogFileSize = size
		report.StartOffset = interval.StartOffset
		report.EndOffset = interval.EndOffset
		report.StopOffset = result.StopOffset
	}

	// Return all query classes if there's no limit or number of classes is
	// less than the limit.
	n := len(result.Class)
	if config.ReportLimit == 0 || n <= int(config.ReportLimit) {
		return report // all classes, no LRQ
	}

	// Top queries
	report.Class = result.Class[0:config.ReportLimit]

	// Low-ranking Queries
	lrq := event.NewQueryClass("0", "", false, 0*time.Second)
	for _, query := range result.Class[config.ReportLimit:n] {
		addQuery(lrq, query)
	}
	report.Class = append(report.Class, lrq)

	return report // top classes, the rest as LRQ
}

func addQuery(dst, src *event.QueryClass) {
	dst.TotalQueries++
	for srcMetric, srcStats := range src.Metrics.TimeMetrics {
		dstStats, ok := dst.Metrics.TimeMetrics[srcMetric]
		if !ok {
			m := *srcStats
			dst.Metrics.TimeMetrics[srcMetric] = &m
		} else {
			dstStats.Sum += srcStats.Sum
			dstStats.Avg = (dstStats.Avg + srcStats.Avg) / 2
			if srcStats.Min < dstStats.Min {
				dstStats.Min = srcStats.Min
			}
			if srcStats.Max > dstStats.Max {
				dstStats.Max = srcStats.Max
			}
		}
	}
}
