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

package query_test

import (
	"database/sql"
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/query"
	"github.com/percona/percona-agent/test/mock"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	nullmysql     *mock.NullMySQL
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	tickChan      chan time.Time
	dataChan      chan interface{}
	traceChan     chan string
	readyChan     chan bool
	configDir     string
	tmpDir        string
	ir            *instance.Repo
	dsn           string
	rir           *instance.Repo
	mysqlInstance proto.ServiceInstance
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	s.nullmysql = mock.NewNullMySQL()
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, query.SERVICE_NAME+"-manager-test")
	s.traceChan = make(chan string, 10)
	s.dataChan = make(chan interface{}, 1)

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	// Fake instance repo
	s.ir = instance.NewRepo(pct.NewLogger(s.logChan, "ir"), s.configDir, s.api)
	data, err := json.Marshal(&proto.MySQLInstance{
		Hostname: "db1",
		DSN:      "user:host@tcp(127.0.0.1:3306)/",
	})
	t.Assert(err, IsNil)
	s.ir.Add("mysql", 1, data, false)

	// Real instance repo
	s.rir = instance.NewRepo(pct.NewLogger(s.logChan, "im-test"), s.configDir, s.api)
	data, err = json.Marshal(&proto.MySQLInstance{
		Hostname: "db1",
		DSN:      s.dsn,
	})
	t.Assert(err, IsNil)
	s.rir.Add("mysql", 1, data, false)
	s.mysqlInstance = proto.ServiceInstance{Service: "mysql", InstanceId: 1}

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	glob := filepath.Join(pct.Basedir.Dir("config"), "*")
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopManager(t *C) {
	var err error

	// Create query manager
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := query.NewManager(s.logger, mockConnFactory, s.ir)
	t.Assert(m, Not(IsNil), Commentf("Make new query.Manager"))

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Its status should be "Running".
	status := m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Running")

	// Can't start manager twice.
	err = m.Start()
	t.Check(err, FitsTypeOf, pct.ServiceIsRunningError{})

	// Explain query
	q := "SELECT 1"
	expectedExplain := []proto.ExplainRow{
		proto.ExplainRow{
			Id: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 1,
					Valid: true,
				},
			},
			SelectType: proto.NullString{
				NullString: sql.NullString{
					String: "SIMPLE",
					Valid:  true,
				},
			},
			Table: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			CreateTable: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Type: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			PossibleKeys: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Key: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			KeyLen: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Ref: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Rows: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Extra: proto.NullString{
				NullString: sql.NullString{
					String: "No tables used",
					Valid:  true,
				},
			},
		},
	}
	s.nullmysql.SetExplain(q, expectedExplain)

	serviceInstance := proto.ServiceInstance{
		Service:    "mysql",
		InstanceId: 1,
	}

	explainQuery := &proto.ExplainQuery{
		ServiceInstance: serviceInstance,
		Query:           q,
	}
	data, err := json.Marshal(&explainQuery)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
		Data:    data,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, "")

	var gotExplain []proto.ExplainRow
	err = json.Unmarshal(gotReply.Data, &gotExplain)
	t.Assert(err, IsNil)
	t.Assert(gotExplain, DeepEquals, expectedExplain)

	// You can't stop this service
	err = m.Stop()
	t.Check(err, IsNil)
	status = m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Running")
}

func (s *ManagerTestSuite) TestExplainWithoutDb(t *C) {
	var err error

	// Create query manager
	m := query.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.rir)
	t.Assert(m, Not(IsNil), Commentf("Make new query.Manager"))

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Explain query
	query := "SELECT 1"
	expectedExplain := []proto.ExplainRow{
		proto.ExplainRow{
			Id: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 1,
					Valid: true,
				},
			},
			SelectType: proto.NullString{
				NullString: sql.NullString{
					String: "SIMPLE",
					Valid:  true,
				},
			},
			Table: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			CreateTable: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Type: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			PossibleKeys: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Key: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			KeyLen: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Ref: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Rows: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Extra: proto.NullString{
				NullString: sql.NullString{
					String: "No tables used",
					Valid:  true,
				},
			},
		},
	}

	explainQuery := &proto.ExplainQuery{
		ServiceInstance: s.mysqlInstance,
		Query:           query,
	}
	data, err := json.Marshal(&explainQuery)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
		Data:    data,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, "")

	var gotExplain []proto.ExplainRow
	err = json.Unmarshal(gotReply.Data, &gotExplain)
	t.Assert(err, IsNil)
	t.Assert(gotExplain, DeepEquals, expectedExplain)
}

func (s *ManagerTestSuite) TestExplainWithDb(t *C) {
	var err error

	// Create query manager
	m := query.NewManager(s.logger, &mysql.RealConnectionFactory{}, s.rir)
	t.Assert(m, Not(IsNil), Commentf("Make new query.Manager"))

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Explain query
	db := "information_schema"
	query := "SELECT table_name FROM tables WHERE table_name='tables'"

	expectedExplain := []proto.ExplainRow{
		proto.ExplainRow{
			Id: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 1,
					Valid: true,
				},
			},
			SelectType: proto.NullString{
				NullString: sql.NullString{
					String: "SIMPLE",
					Valid:  true,
				},
			},
			Table: proto.NullString{
				NullString: sql.NullString{
					String: "tables",
					Valid:  true,
				},
			},
			CreateTable: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Type: proto.NullString{
				NullString: sql.NullString{
					String: "ALL",
					Valid:  true,
				},
			},
			PossibleKeys: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Key: proto.NullString{
				NullString: sql.NullString{
					String: "TABLE_NAME",
					Valid:  true,
				},
			},
			KeyLen: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Ref: proto.NullString{
				NullString: sql.NullString{
					String: "",
					Valid:  false,
				},
			},
			Rows: proto.NullInt64{
				NullInt64: sql.NullInt64{
					Int64: 0,
					Valid: false,
				},
			},
			Extra: proto.NullString{
				NullString: sql.NullString{
					String: "Using where; Skip_open_table; Scanned 1 database",
					Valid:  true,
				},
			},
		},
	}

	explainQuery := &proto.ExplainQuery{
		ServiceInstance: s.mysqlInstance,
		Db:              db,
		Query:           query,
	}
	data, err := json.Marshal(&explainQuery)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
		Data:    data,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, "")

	var gotExplain []proto.ExplainRow
	err = json.Unmarshal(gotReply.Data, &gotExplain)
	t.Assert(err, IsNil)
	t.Assert(gotExplain, DeepEquals, expectedExplain)
}
