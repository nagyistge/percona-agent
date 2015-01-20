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
	"path/filepath"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/cloud-protocol/proto"
	//"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	"github.com/percona/percona-agent/qan/perfschema"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var inputDir = RootDir() + "/test/qan/perfschema/"
var outputDir = RootDir() + "/test/qan/perfschema/"

type WorkerTestSuite struct {
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	nullmysql *mock.NullMySQL
}

var _ = Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) SetUpSuite(t *C) {
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
	return func(c chan<- *perfschema.DigestRow) error {
		if len(iters) == 0 {
			return fmt.Errorf("No more iters")
		}
		rows := iters[0]
		iters = iters[1:len(iters)]
		go func() {
			defer close(c)
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

/*
func (s *PfsWorkerTestSuite) TestCollectData(t *C) {
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
	mysqlConn := mysql.NewConnection(s.dsn)
	err := mysqlConn.Connect(1)
	t.Assert(err, IsNil)
	defer mysqlConn.Close()

	enablePfs := []mysql.Query{
		mysql.Query{Verify: "performance_schema", Expect: "1"},
		mysql.Query{Set: "UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'"},
		mysql.Query{Set: "UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'"},
		mysql.Query{Set: "TRUNCATE performance_schema.events_statements_summary_by_digest"},
	}
	if err := mysqlConn.Set(enablePfs); err != nil {
		t.Fatal(err)
	}

	db := mysqlConn.DB()
	_, err = db.Exec("SELECT NOW()")
	_, err = db.Exec("SELECT 1")
	_, err = db.Exec("SELECT * FROM `events_statements_summary_by_digest`")

	 // as we don't have consistent order in maps from Go v1.3,
	 //let's use pre-defined map with queries
	expectedResult := make(map[string]bool)
	expectedResult["TRUNCATE `performance_schema` . `events_statements_summary_by_digest` "] = true
	expectedResult["SELECT NOW ( ) "] = true
	expectedResult["SELECT ? "] = true
	expectedResult["SELECT * FROM `events_statements_summary_by_digest` "] = true

	w := qan.NewPfsWorker(s.logger, "pfs-worker", mysqlConn)
	gotPfsData, err := w.CollectData()
	t.Assert(err, IsNil)
	t.Assert(gotPfsData, NotNil)
	for i := range gotPfsData {
		if !expectedResult[gotPfsData[i].DigestText] {
			t.Errorf("Missing %s", gotPfsData[i].DigestText)
			Dump(gotPfsData)
		}
	}
}

func (s *PfsWorkerTestSuite) TestPrepareResult001(t *C) {
	parsedTime, _ := time.Parse("2006-01-02T15:04:05Z", "2014-07-10T19:14:30Z")
	pfsData := []*qan.PfsRow{
		{
			Digest:                  "d082a30b349166452cd1148310124d77",
			DigestText:              "TRUNCATE `events_statements_summary_by_digest` ",
			SumTimerWait:            588631000,
			MinTimerWait:            588631000,
			AvgTimerWait:            588631000,
			MaxTimerWait:            588631000,
			SumLockTime:             119000000,
			SumRowsAffected:         0,
			SumRowsSent:             0,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "973f7f10f95fc62e80148f2845ceca42",
			DigestText:              "SELECT NOW ( ) ",
			SumTimerWait:            41687000,
			MinTimerWait:            41687000,
			AvgTimerWait:            41687000,
			MaxTimerWait:            41687000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             1,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "93eaedb019bdfcf59f7aea8a25486ef0",
			DigestText:              "SELECT ? ",
			SumTimerWait:            20274000,
			MinTimerWait:            20274000,
			AvgTimerWait:            20274000,
			MaxTimerWait:            20274000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             1,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "fcc2b877639138358ef059551099f0d0",
			DigestText:              "SELECT * FROM `events_statements_summary_by_digest` ",
			SumTimerWait:            155949000,
			MinTimerWait:            155949000,
			AvgTimerWait:            155949000,
			MaxTimerWait:            155949000,
			SumLockTime:             38000000,
			SumRowsAffected:         0,
			SumRowsSent:             3,
			SumRowsExamined:         3,
			SumSelectFullJoin:       0,
			SumSelectScan:           1,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
		{
			Digest:                  "2ea7017783cf24845827fd4e2cff1a5b",
			DigestText:              "USE `performance_schema` ",
			SumTimerWait:            24017000,
			MinTimerWait:            24017000,
			AvgTimerWait:            24017000,
			MaxTimerWait:            24017000,
			SumLockTime:             0,
			SumRowsAffected:         0,
			SumRowsSent:             0,
			SumRowsExamined:         0,
			SumSelectFullJoin:       0,
			SumSelectScan:           0,
			SumSortMergePasses:      0,
			SumCreatedTmpDiskTables: 0,
			SumCreatedTmpTables:     0,
			CountStar:               1,
			FirstSeen:               parsedTime,
			LastSeen:                parsedTime,
		},
	}

	w := qan.NewPfsWorker(s.logger, "pfs-worker", mock.NewNullMySQL())
	got, err := w.PrepareResult(pfsData)
	t.Assert(err, IsNil)
	t.Assert(got, NotNil)
	expect := &qan.Result{}
	err = test.LoadMmReport(outputDir+"pfs001.json", expect)
	t.Assert(err, IsNil)
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}
*/
