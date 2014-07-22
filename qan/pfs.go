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
	"database/sql"
	"fmt"
	"github.com/percona/mysql-log-parser/log"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"math"
	"time"
)

type PfsRow struct {
	Digest, DigestText                                         string
	SumTimerWait, MinTimerWait, AvgTimerWait, MaxTimerWait     uint64
	SumLockTime, SumRowsAffected, SumRowsSent, SumRowsExemined uint64
	SumSelectFullJoin, SumSelectScan, SumSortMergePasses       uint
	SumCreatedTmpDiskTables, SumCreatedTmpTables, CountStar    uint
	FirstSeen, LastSeen                                        time.Time
}

type PfsWorker struct {
	logger    *pct.Logger
	name      string
	mysqlConn mysql.Connector
	status    *pct.Status
}

func NewPfsWorker(logger *pct.Logger, name string, mysqlConn mysql.Connector) *PfsWorker {
	w := &PfsWorker{
		logger:    logger,
		name:      name,
		mysqlConn: mysqlConn,
		status:    pct.NewStatus([]string{name}),
	}
	return w
}

func (w *PfsWorker) Name() string {
	return w.name
}

func (w *PfsWorker) Status() string {
	return w.status.Get(w.name)
}

func (w *PfsWorker) Run(job *Job) (*Result, error) {
	defer w.status.Update(w.name, "Idle")

	w.status.Update(w.name, "Connecting to MySQL")
	if err := w.mysqlConn.Connect(2); err != nil {
		return nil, err
	}
	defer w.mysqlConn.Close()

	rows, err := w.CollectData()
	if err != nil {
		return nil, err
	}
	if err := w.TruncateTable(); err != nil {
		return nil, err
	}
	return w.PrepareResult(rows)
}

func (w *PfsWorker) CollectData() ([]*PfsRow, error) {
	w.status.Update(w.name, "SELECT performance_schema.events_statements_summary_by_digest")

	query := "SELECT " +
		"DIGEST, DIGEST_TEXT, COUNT_STAR, " +
		"SUM_TIMER_WAIT, MIN_TIMER_WAIT, AVG_TIMER_WAIT, " +
		"MAX_TIMER_WAIT, SUM_LOCK_TIME, SUM_ROWS_AFFECTED, " +
		"SUM_ROWS_SENT, SUM_ROWS_EXAMINED, SUM_CREATED_TMP_DISK_TABLES, " +
		"SUM_CREATED_TMP_TABLES, SUM_SELECT_FULL_JOIN, SUM_SELECT_SCAN, " +
		"SUM_SORT_MERGE_PASSES, FIRST_SEEN, LAST_SEEN " +
		"FROM performance_schema.events_statements_summary_by_digest"
	rows, err := w.mysqlConn.DB().Query(query)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	data := []*PfsRow{}
	for rows.Next() {
		row := &PfsRow{}
		err := rows.Scan(
			&row.Digest, &row.DigestText, &row.CountStar,
			&row.SumTimerWait, &row.MinTimerWait, &row.AvgTimerWait, &row.MaxTimerWait, &row.SumLockTime,
			&row.SumRowsAffected, &row.SumRowsSent, &row.SumRowsExemined, &row.SumCreatedTmpDiskTables, &row.SumCreatedTmpTables,
			&row.SumSelectFullJoin, &row.SumSelectScan, &row.SumSortMergePasses, &row.FirstSeen, &row.LastSeen,
		)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan error: %s: ", err)
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err error: %s: ", err)
	}
	return data, nil
}

func (w *PfsWorker) TruncateTable() error {
	w.status.Update(w.name, "TRUNCATE performance_schema.events_statements_summary_by_digest")
	_, err := w.mysqlConn.DB().Exec("TRUNCATE performance_schema.events_statements_summary_by_digest")
	return err
}

func (w *PfsWorker) PrepareResult(rows []*PfsRow) (*Result, error) {
	w.status.Update(w.name, "Preparing result")
	result := &Result{}
	event := log.NewEvent()
	global := log.NewGlobalClass()
	for _, row := range rows {
		eventStat := log.NewEventStats()
		event.Query = row.DigestText
		event.Ts = fmt.Sprintf("%s", row.LastSeen)

		eventStat.TimeMetrics["Query_time"] = &log.TimeStats{}
		eventStat.TimeMetrics["Query_time"].Sum = float64(row.SumTimerWait) * math.Pow10(-6)
		eventStat.TimeMetrics["Query_time"].Min = float64(row.MinTimerWait) * math.Pow10(-6)
		eventStat.TimeMetrics["Query_time"].Max = float64(row.MaxTimerWait) * math.Pow10(-6)
		eventStat.TimeMetrics["Query_time"].Avg = float64(row.AvgTimerWait) * math.Pow10(-6)
		eventStat.TimeMetrics["Query_time"].Cnt = row.CountStar

		eventStat.TimeMetrics["Lock_time"] = &log.TimeStats{}
		eventStat.TimeMetrics["Lock_time"].Sum = float64(row.SumLockTime) * math.Pow10(-6)

		eventStat.NumberMetrics["Rows_affected"] = &log.NumberStats{}
		eventStat.NumberMetrics["Rows_affected"].Sum = row.SumRowsAffected

		eventStat.NumberMetrics["Rows_sent"] = &log.NumberStats{}
		eventStat.NumberMetrics["Rows_sent"].Sum = row.SumRowsSent

		eventStat.NumberMetrics["Rows_examined"] = &log.NumberStats{}
		eventStat.NumberMetrics["Rows_examined"].Sum = row.SumRowsExemined

		eventStat.NumberMetrics["Tmp_table_on_disk"] = &log.NumberStats{}
		eventStat.NumberMetrics["Tmp_table_on_disk"].Cnt = row.SumCreatedTmpDiskTables

		eventStat.NumberMetrics["Tmp_table"] = &log.NumberStats{}
		eventStat.NumberMetrics["Tmp_table"].Cnt = row.SumCreatedTmpTables

		eventStat.NumberMetrics["Full_join"] = &log.NumberStats{}
		eventStat.NumberMetrics["Full_join"].Cnt = row.SumSelectFullJoin

		eventStat.NumberMetrics["Full_scan"] = &log.NumberStats{}
		eventStat.NumberMetrics["Full_scan"].Cnt = row.SumSelectScan

		eventStat.NumberMetrics["Merge_passes"] = &log.NumberStats{}
		eventStat.NumberMetrics["Merge_passes"].Cnt = row.SumSortMergePasses

		classId := log.Checksum(row.DigestText)
		class := log.NewQueryClass(classId, row.DigestText, false)
		class.AddEvent(event)
		class.Metrics = eventStat
		result.Classes = append(result.Classes, class)
	}
	result.Global = global
	return result, nil
}
