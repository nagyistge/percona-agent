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

package perfschema_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/qan/perfschema"
	//"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var inputDir = RootDir() + "/test/qan/perfschema/"
var outputDir = RootDir() + "/test/qan/perfschema/"

type WorkerTestSuite struct {
	dsn       string
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	nullmysql *mock.NullMySQL
}

var _ = Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
	s.nullmysql = mock.NewNullMySQL()
}

func (s *WorkerTestSuite) SetUpTest(t *C) {
	s.nullmysql.Reset()
}

func (s *WorkerTestSuite) loadData(dir string) ([][]*perfschema.DigestRow, error) {
	files, err := filepath.Glob(filepath.Join(inputDir, dir, "/iter*.json"))
	if err != nil {
		return nil, err
	}
	iters := [][]*perfschema.DigestRow{}
	for _, file := range files {
		bytes, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rows := []*perfschema.DigestRow{}
		if err := json.Unmarshal(bytes, &rows); err != nil {
			return nil, err
		}
		iters = append(iters, rows)
	}
	return iters, nil
}

func (s *WorkerTestSuite) loadResult(file string) (*qan.Result, error) {
	file = filepath.Join(inputDir, file)
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	res := &qan.Result{}
	if err := json.Unmarshal(bytes, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func makeGetRowsFunc(iters [][]*perfschema.DigestRow) perfschema.GetDigestRowsFunc {
	return func(c chan<- *perfschema.DigestRow, done chan<- error) error {
		if len(iters) == 0 {
			return fmt.Errorf("No more iters")
		}
		rows := iters[0]
		iters = iters[1:len(iters)]
		go func() {
			defer func() {
				done <- nil
			}()
			for _, row := range rows {
				c <- row
			}
		}()
		return nil
	}
}

func makeGetTextFunc(texts ...string) perfschema.GetDigestTextFunc {
	return func(digest string) (string, error) {
		if len(texts) == 0 {
			return "", fmt.Errorf("No more texts")
		}
		text := texts[0]
		texts = texts[1:len(texts)]
		return text, nil
	}
}

// --------------------------------------------------------------------------

func (s *WorkerTestSuite) Test001(t *C) {
	// This is the simplest input possible: 1 query in iter 1 and 2. The result
	// is just the increase in its values.

	rows, err := s.loadData("001")
	t.Assert(err, IsNil)
	getRows := makeGetRowsFunc(rows)
	getText := makeGetTextFunc("select 1")
	w := perfschema.NewWorker(s.logger, s.nullmysql, getRows, getText)

	// First run doesn't produce a result because 2 snapshots are required.
	i := &qan.Interval{
		Number:    1,
		StartTime: time.Now().UTC(),
	}
	err = w.Setup(i)
	t.Assert(err, IsNil)

	res, err := w.Run()
	t.Assert(err, IsNil)
	t.Check(res, IsNil)

	err = w.Cleanup()
	t.Assert(err, IsNil)

	// The second run produces a result: the diff of 2nd - 1st.
	i = &qan.Interval{
		Number:    2,
		StartTime: time.Now().UTC(),
	}
	err = w.Setup(i)
	t.Assert(err, IsNil)

	res, err = w.Run()
	t.Assert(err, IsNil)
	expect, err := s.loadResult("001/res01.json")
	t.Assert(err, IsNil)
	if same, diff := IsDeeply(res, expect); !same {
		Dump(diff)
		t.Error(diff)
	}

	err = w.Cleanup()
	t.Assert(err, IsNil)
}

func (s *WorkerTestSuite) Test002(t *C) {
	// This is the 2nd most simplest input after 001: two queries, same digest,
	// but different schemas. The reuslt is the aggregate of their value diffs
	// from iter 1 to 2.

	rows, err := s.loadData("002")
	t.Assert(err, IsNil)
	getRows := makeGetRowsFunc(rows)
	getText := makeGetTextFunc("select 1")
	w := perfschema.NewWorker(s.logger, s.nullmysql, getRows, getText)

	// First run doesn't produce a result because 2 snapshots are required.
	i := &qan.Interval{
		Number:    1,
		StartTime: time.Now().UTC(),
	}
	err = w.Setup(i)
	t.Assert(err, IsNil)

	res, err := w.Run()
	t.Assert(err, IsNil)
	t.Check(res, IsNil)

	err = w.Cleanup()
	t.Assert(err, IsNil)

	// The second run produces a result: the diff of 2nd - 1st.
	i = &qan.Interval{
		Number:    2,
		StartTime: time.Now().UTC(),
	}
	err = w.Setup(i)
	t.Assert(err, IsNil)

	res, err = w.Run()
	t.Assert(err, IsNil)
	expect, err := s.loadResult("002/res01.json")
	t.Assert(err, IsNil)
	if same, diff := IsDeeply(res, expect); !same {
		Dump(res)
		t.Error(diff)
	}

	err = w.Cleanup()
	t.Assert(err, IsNil)
}

func (s *WorkerTestSuite) TestRealWorker(t *C) {
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	mysqlConn := mysql.NewConnection(s.dsn)
	err := mysqlConn.Connect(1)
	t.Assert(err, IsNil)
	defer mysqlConn.Close()

	f := perfschema.NewRealWorkerFactory(s.logChan)
	w := f.Make("qan-worker", mysqlConn)

	start := []mysql.Query{
		mysql.Query{Verify: "performance_schema", Expect: "1"},
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'"},
		mysql.Query{Set: "TRUNCATE performance_schema.events_statements_summary_by_digest"},
	}
	if err := mysqlConn.Set(start); err != nil {
		t.Fatal(err)
	}
	stop := []mysql.Query{
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'NO' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'NO', TIMED = 'NO' WHERE NAME LIKE 'statement/sql/%'"},
	}
	defer func() {
		if err := mysqlConn.Set(stop); err != nil {
			t.Fatal(err)
		}
	}()

	// SCHEMA_NAME: NULL
	//      DIGEST: fbe070dfb47e4a2401c5be6b5201254e
	// DIGEST_TEXT: SELECT ? FROM DUAL
	_, err = mysqlConn.DB().Exec("SELECT 'teapot' FROM DUAL")

	// First interval.
	err = w.Setup(&qan.Interval{Number: 1, StartTime: time.Now().UTC()})
	t.Assert(err, IsNil)

	res, err := w.Run()
	t.Assert(err, IsNil)
	t.Check(res, IsNil)

	err = w.Cleanup()
	t.Assert(err, IsNil)

	// Some query activity between intervals.
	_, err = mysqlConn.DB().Exec("SELECT 'teapot' FROM DUAL")
	time.Sleep(1 * time.Second)

	// Second interval and a result.
	err = w.Setup(&qan.Interval{Number: 1, StartTime: time.Now().UTC()})
	t.Assert(err, IsNil)

	res, err = w.Run()
	t.Assert(res, NotNil)
	if len(res.Class) == 0 {
		t.Fatal("Expected len(res.Class) > 0")
	}
	t.Check(res.Class, HasLen, 1)
	class := res.Class[0]
	// Digests on different versions or distros of MySQL don't match
	//t.Check(class.Id, Equals, "01C5BE6B5201254E")
	t.Check(class.Fingerprint, Equals, "SELECT ? FROM DUAL ")
	queryTime := class.Metrics.TimeMetrics["Query_time"]
	if queryTime.Min == 0 {
		t.Error("Expected Query_time_min > 0")
	}
	if queryTime.Max == 0 {
		t.Error("Expected Query_time_max > 0")
	}
	if queryTime.Avg == 0 {
		t.Error("Expected Query_time_avg > 0")
	}
	if queryTime.Min > queryTime.Max {
		t.Error("Expected Query_time_min >= Query_time_max")
	}
	t.Check(class.Metrics.NumberMetrics["Rows_affected"].Sum, Equals, uint64(0))
	t.Check(class.Metrics.NumberMetrics["Rows_examined"].Sum, Equals, uint64(0))
	t.Check(class.Metrics.NumberMetrics["Rows_sent"].Sum, Equals, uint64(1))

	err = w.Cleanup()
	t.Assert(err, IsNil)
}

func (s *WorkerTestSuite) TestIter(t *C) {
	tickChan := make(chan time.Time, 1)
	i := perfschema.NewIter(pct.NewLogger(s.logChan, "iter"), tickChan)
	t.Assert(i, NotNil)

	iterChan := i.IntervalChan()
	t.Assert(iterChan, NotNil)

	i.Start()
	defer i.Stop()

	t1, _ := time.Parse("2006-01-02 15:04:05", "2015-01-01 00:01:00")
	t2, _ := time.Parse("2006-01-02 15:04:05", "2015-01-01 00:02:00")
	t3, _ := time.Parse("2006-01-02 15:04:05", "2015-01-01 00:03:00")

	tickChan <- t1
	got := <-iterChan
	t.Check(got, DeepEquals, &qan.Interval{Number: 1, StartTime: time.Time{}, StopTime: t1})

	tickChan <- t2
	got = <-iterChan
	t.Check(got, DeepEquals, &qan.Interval{Number: 2, StartTime: t1, StopTime: t2})

	tickChan <- t3
	got = <-iterChan
	t.Check(got, DeepEquals, &qan.Interval{Number: 3, StartTime: t2, StopTime: t3})
}
