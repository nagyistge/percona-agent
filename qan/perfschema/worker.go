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

package perfschema

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/go-mysql/event"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
)

type WorkerFactory interface {
	Make(name string, mysqlConn mysql.Connector) qan.Worker
}

type RealWorkerFactory struct {
	logChan chan *proto.LogEntry
}

func NewRealWorkerFactory(logChan chan *proto.LogEntry) *RealWorkerFactory {
	f := &RealWorkerFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealWorkerFactory) Make(name string, mysqlConn mysql.Connector) *Worker {
	return NewWorker(pct.NewLogger(f.logChan, name), mysqlConn)
}

// A DigestRow is a row from performance_schema.events_statements_summary_by_digest.
type DigestRow struct {
	CountStar               uint
	SumTimerWait            uint
	MinTimerWait            uint
	AvgTimerWait            uint
	MaxTimerWait            uint
	SumLockTime             uint
	SumRowsAffected         uint64
	SumRowsSent             uint64
	SumRowsExamined         uint64
	SumCreatedTmpDiskTables uint
	SumCreatedTmpTables     uint
	SumSelectFullJoin       uint
	SumSelectScan           uint
	SumSortMergePasses      uint64
	FirstSeen               time.Time
	LastSeen                time.Time
}

type Class struct {
	DigestText string
	Rows       map[string]*DigestRow // keyed on schema
}

type Snapshot struct {
	Interval *qan.Interval
	Classes  map[string]Class // keyed on digest (class ID)
}

type Worker struct {
	logger    *pct.Logger
	mysqlConn mysql.Connector
	// --
	name   string
	status *pct.Status
	sync   *pct.SyncChan
	prev   *Snapshot
	curr   *Snapshot
}

func NewWorker(logger *pct.Logger, mysqlConn mysql.Connector) *Worker {
	name := logger.Service()
	w := &Worker{
		logger:    logger,
		mysqlConn: mysqlConn,
		// --
		name:   name,
		status: pct.NewStatus([]string{name}),
		sync:   pct.NewSyncChan(),
		curr: &Snapshot{
			Classes: make(map[string]Class),
		},
	}
	return w
}

func (w *Worker) Setup(interval *qan.Interval) error {
	w.prev = w.curr
	w.curr = &Snapshot{
		Interval: interval,
		Classes:  make(map[string]Class),
	}
	return nil
}

func (w *Worker) Run() (*qan.Result, error) {
	if err := w.CollectData(); err != nil {
		return nil, err
	}
	return w.PrepareResult()
}

func (w *Worker) Stop() error {
	w.sync.Stop()
	w.sync.Wait()
	return nil
}

func (w *Worker) Cleanup() error {
	return nil
}

func (w *Worker) Status() map[string]string {
	return w.status.All()
}

// --------------------------------------------------------------------------

func (w *Worker) CollectData() error {
	w.status.Update(w.name, "SELECT performance_schema.events_statements_summary_by_digest")
	rows, err := w.mysqlConn.DB().Query(
		"SELECT " +
			" COALESCE(SCHEMA_NAME, ''), COALESCE(DIGEST, ''), COUNT_STAR," +
			" SUM_TIMER_WAIT, MIN_TIMER_WAIT, AVG_TIMER_WAIT, MAX_TIMER_WAIT," +
			" SUM_LOCK_TIME," +
			" SUM_ROWS_AFFECTED, SUM_ROWS_SENT, SUM_ROWS_EXAMINED," +
			" SUM_CREATED_TMP_DISK_TABLES, SUM_CREATED_TMP_TABLES," +
			" SUM_SELECT_FULL_JOIN, SUM_SELECT_SCAN, SUM_SORT_MERGE_PASSES," +
			" FIRST_SEEN, LAST_SEEN" +
			" FROM performance_schema.events_statements_summary_by_digest")
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			schema string
			digest string
		)
		row := &DigestRow{}
		err := rows.Scan(
			&schema,
			&digest,
			&row.CountStar,
			&row.SumTimerWait,
			&row.MinTimerWait,
			&row.AvgTimerWait,
			&row.MaxTimerWait,
			&row.SumLockTime,
			&row.SumRowsAffected,
			&row.SumRowsSent,
			&row.SumRowsExamined,
			&row.SumCreatedTmpDiskTables,
			&row.SumCreatedTmpTables,
			&row.SumSelectFullJoin,
			&row.SumSelectScan,
			&row.SumSortMergePasses,
			&row.FirstSeen,
			&row.LastSeen,
		)
		if err != nil {
			w.logger.Warn(err)
			continue
		}
		classId := strings.ToUpper(digest[16:32])
		if class, haveClass := w.curr.Classes[classId]; haveClass {
			if _, haveRow := class.Rows[schema]; haveRow {
				w.logger.Error("Got class twice: ", schema, digest)
				continue
			}
			class.Rows[schema] = row
		} else {
			// Get class digext text (fingerprint).
			var digestText string
			if prevClass, havePrevClass := w.prev.Classes[classId]; havePrevClass {
				// Class was in previous iter, so re-use its digest text.
				digestText = prevClass.DigestText
			} else {
				// Have never seen class before, so get digext text from perf schema.
				var err error
				digestText, err = w.getDigestText(digest)
				if err != nil {
					w.logger.Error(err)
					continue
				}
			}
			// Create the class and init with this schema and row.
			w.curr.Classes[classId] = Class{
				DigestText: digestText,
				Rows: map[string]*DigestRow{
					schema: row,
				},
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows.Err error: %s: ", err)
	}
	return nil
}

func (w *Worker) PrepareResult() (*qan.Result, error) {
	w.status.Update(w.name, "Preparing result")

	global := event.NewGlobalClass()
	classes := []*event.QueryClass{}

	// Compare current classes to previous.
CLASS_LOOP:
	for classId, class := range w.curr.Classes {

		// If this class does not exist in prev, skip the entire class.
		prevClass, ok := w.prev.Classes[classId]
		if !ok {
			continue CLASS_LOOP
		}

		// This class exists in prev, so create a class aggregate of the per-schema
		// query value diffs, for rows that exist in both prev and curr.
		d := DigestRow{} // class aggregate, becomes class metrics
		n := uint(0)     // total queries in this class

		// Each row is an instance of the query executed in the schema.
	ROW_LOOP:
		for schema, row := range class.Rows {

			// If the row does not exist in prev, skip it. This means this is
			// the first time we've seen this query in this schema.
			prevRow, ok := prevClass.Rows[schema]
			if !ok {
				continue ROW_LOOP
			}

			// If query count has not changed, skip it. This means the query was
			// not executed between prev and curr interval.
			if row.CountStar == prevRow.CountStar {
				continue ROW_LOOP
			}

			// This per-schema query exists in both prev and curr, so add the diff
			// of its values to the class aggregate.
			n++

			// Add the diff of the totals to the class metric totals. For example,
			// if query 1 in db1 has prev.CountStar=50 and curr.CountStar=100,
			// and query 1 in db2 has prev.CountStar=100 and curr.CountStar=200,
			// that's +50 and +100 executions respectively, so +150 executions for
			// the class metrics.
			d.CountStar += row.CountStar - prevRow.CountStar
			d.SumTimerWait += row.SumTimerWait - prevRow.SumTimerWait
			d.SumLockTime += row.SumLockTime - prevRow.SumLockTime
			d.SumRowsAffected += row.SumRowsAffected - prevRow.SumRowsAffected
			d.SumRowsSent += row.SumRowsSent - prevRow.SumRowsSent
			d.SumRowsExamined += row.SumRowsExamined - prevRow.SumRowsExamined
			d.SumCreatedTmpDiskTables += row.SumCreatedTmpDiskTables - prevRow.SumCreatedTmpDiskTables
			d.SumCreatedTmpTables += row.SumCreatedTmpTables - prevRow.SumCreatedTmpTables
			d.SumSelectFullJoin += row.SumSelectFullJoin - prevRow.SumSelectFullJoin
			d.SumSelectScan += row.SumSelectScan - prevRow.SumSelectScan
			d.SumSortMergePasses += row.SumSortMergePasses - prevRow.SumSortMergePasses

			// Take the current min and max.
			if row.MinTimerWait < d.MinTimerWait {
				d.MinTimerWait = row.MinTimerWait
			}
			if row.MaxTimerWait > d.MaxTimerWait {
				d.MaxTimerWait = row.MaxTimerWait
			}
			// Add the averages, divide later.
			d.MaxTimerWait += row.MaxTimerWait
		}

		// Divide the total averages to yield the average of the averages.
		d.MaxTimerWait /= n // todo: d.CountStar instead of n?

		// Create standard metric stats from the class metrics just calculated.
		stats := event.NewMetrics()

		// Time metircs, in picoseconds (x10^-12 to convert to seconds)
		stats.TimeMetrics["Query_time"] = &event.TimeStats{
			Cnt: d.CountStar,
			Sum: float64(d.SumTimerWait) * math.Pow10(-12),
			Min: float64(d.MinTimerWait) * math.Pow10(-12),
			Max: float64(d.MaxTimerWait) * math.Pow10(-12),
			Avg: float64(d.AvgTimerWait) * math.Pow10(-12),
		}

		stats.TimeMetrics["Lock_time"] = &event.TimeStats{
			Cnt: d.CountStar,
			Sum: float64(d.SumLockTime) * math.Pow10(-12),
		}

		// Number metrics
		stats.NumberMetrics["Rows_affected"] = &event.NumberStats{
			Cnt: d.CountStar,
			Sum: d.SumRowsAffected,
		}

		stats.NumberMetrics["Rows_sent"] = &event.NumberStats{
			Cnt: d.CountStar,
			Sum: d.SumRowsSent,
		}

		stats.NumberMetrics["Rows_examined"] = &event.NumberStats{
			Cnt: d.CountStar,
			Sum: d.SumRowsExamined,
		}

		stats.NumberMetrics["Merge_passes"] = &event.NumberStats{
			Cnt: d.CountStar,
			Sum: d.SumSortMergePasses,
		}

		// Bool metrics
		stats.BoolMetrics["Tmp_table_on_disk"] = &event.BoolStats{
			Cnt:  d.CountStar,
			True: d.SumCreatedTmpDiskTables,
		}

		stats.BoolMetrics["Tmp_table"] = &event.BoolStats{
			Cnt:  d.CountStar,
			True: d.SumCreatedTmpTables,
		}

		stats.BoolMetrics["Full_join"] = &event.BoolStats{
			Cnt:  d.CountStar,
			True: d.SumSelectFullJoin,
		}

		stats.BoolMetrics["Full_scan"] = &event.BoolStats{
			Cnt:  d.CountStar,
			True: d.SumSelectScan,
		}

		// Create and save the pre-aggregated class.  Using only last 16 digits
		// of checksum is historical: pt-query-digest does the same:
		// my $checksum = uc substr(md5_hex($val), -16);
		class := event.NewQueryClass(classId, class.DigestText, false)
		class.TotalQueries = uint64(n) // todo: d.CountStar instead of n?
		class.Metrics = stats
		classes = append(classes, class)

		// Add the class to the global metrics.
		global.AddClass(class)
	}

	// Each row/class was unique, so update the global counts.
	nClasses := uint64(len(classes))
	global.TotalQueries = nClasses
	global.UniqueQueries = nClasses

	result := &qan.Result{
		Global: global,
		Class:  classes,
	}

	return result, nil
}

func (w *Worker) TruncateTable() error {
	w.status.Update(w.name, "TRUNCATE performance_schema.events_statements_summary_by_digest")
	_, err := w.mysqlConn.DB().Exec("TRUNCATE performance_schema.events_statements_summary_by_digest")
	return err
}

func (w *Worker) getDigestText(digest string) (string, error) {
	query := fmt.Sprintf("SELECT DIGEST_TEXT"+
		" FROM performance_schema.events_statements_summary_by_digest"+
		" WHERE DIGEST='%s' LIMIT 1", digest)
	var digestText string
	err := w.mysqlConn.DB().QueryRow(query).Scan(&digestText)
	return digestText, err
}
