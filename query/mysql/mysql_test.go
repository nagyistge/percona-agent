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

package mysql_test

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/query"
	monitor "github.com/percona/percona-agent/query/mysql"
	"github.com/percona/percona-agent/test"
	. "launchpad.net/gocheck"
	"os"
	"testing"
)

/**
 * This must be set, else all tests will fail.
 */
var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	db      *sql.DB
	logChan chan *proto.LogEntry
	logger  *pct.Logger
	name    string
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	// Get our own connection to MySQL.
	db, err := test.ConnectMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "query-manager-test")
	s.name = "query-mysql-db1"
}

func (s *TestSuite) TearDownSuite(t *C) {
	if s.db != nil {
		s.db.Close()
	}
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestQuery(t *C) {
	// First, monitors monitor an instance of some service (MySQL, RabbitMQ, etc.)
	// So the instance name and id are given.  Second, every monitor has its own
	// specific config info which is sent as the proto.Cmd.Data.  This config
	// embed a query.Config which embed an instance.Config:
	config := &monitor.Config{
		Config: query.Config{
			ServiceInstance: proto.ServiceInstance{
				Service:    "mysql",
				InstanceId: 1,
			},
		},
	}

	// From the config, the factory determine's the monitor's name based on
	// the service instance it's monitoring, and it creates a mysql.Connector
	// for the DSN for that service (since it's a MySQL monitor in this case).
	// It creates the monitor with these args:
	m := monitor.NewMonitor(s.name, config, s.logger, mysql.NewConnection(dsn))
	if m == nil {
		t.Fatal("Make new mysql.Monitor")
	}

	// The factory returns the monitor to the manager which starts it:
	err := m.Start()
	if err != nil {
		t.Fatalf("Start monitor without error, got %s", err)
	}

	// monitor=Ready once it has successfully started and is ready for further instructions
	if ok := test.WaitStatus(1, m, s.name, "Ready"); !ok {
		t.Fatalf("Monitor is not running. Last status: %s", m.Status())
	}
	expectedExplain := &mysql.Explain{
		Result: []mysql.ExplainRow{
			mysql.ExplainRow{
				Id: 1,
				SelectType: mysql.NullString{
					NullString: sql.NullString{
						String: "SIMPLE",
						Valid:  true,
					},
				},
				Table: mysql.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Type: mysql.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				PossibleKeys: mysql.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Key: mysql.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				KeyLen: mysql.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 0,
						Valid: false,
					},
				},
				Ref: mysql.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Rows: mysql.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 0,
						Valid: false,
					},
				},
				Extra: mysql.NullString{
					NullString: sql.NullString{
						String: "No tables used",
						Valid:  true,
					},
				},
			},
		},
	}

	gotExplain, err := m.Explain("SELECT 1")
	t.Assert(err, IsNil)
	t.Assert(gotExplain, DeepEquals, expectedExplain)

	/**
	 * Stop the monitor.
	 */
	m.Stop()
	if ok := test.WaitStatus(1, m, s.name, "Stopped"); !ok {
		t.Fatal("Monitor is not stopped. Last status: %s", m.Status())
	}
}
