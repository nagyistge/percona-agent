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

package qan

import (
	mysqlLog "github.com/percona/percona-go-mysql/log"
	"sort"
	"time"
)

type Report struct {
	StartTs     time.Time // UTC
	EndTs       time.Time // UTC
	SlowLogFile string    // not slow_query_log_file if rotated
	StartOffset int64     // parsing starts
	EndOffset   int64     // parsing stops, but...
	StopOffset  int64     // ...parsing didn't complete if stop < end
	RunTime     float64   // seconds
	Global      *mysqlLog.GlobalClass
	Class       []*mysqlLog.QueryClass
}

type ByQueryTime []*mysqlLog.QueryClass

func (a ByQueryTime) Len() int      { return len(a) }
func (a ByQueryTime) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByQueryTime) Less(i, j int) bool {
	return a[i].Metrics.TimeMetrics["Query_time"].Sum < a[j].Metrics.TimeMetrics["Query_time"].Sum
}

func MakeReport(interval *Interval, result *Result, config *Config) *Report {

	sort.Sort(ByQueryTime(result.Classes))

	report := &Report{
		StartTs:     interval.StartTime,
		EndTs:       interval.StopTime,
		SlowLogFile: interval.Filename,
		StartOffset: interval.StartOffset,
		EndOffset:   interval.EndOffset,
		StopOffset:  result.StopOffset,
		RunTime:     result.RunTime.Seconds(),
		Global:      result.Global,
		Class:       result.Classes,
	}

	return report
}
