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
	mysqlMonitor "github.com/percona/percona-agent/query/mysql"
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
	logChan      chan *proto.LogEntry
	logger       *pct.Logger
	tickChan     chan time.Time
	dataChan     chan interface{}
	traceChan    chan string
	readyChan    chan bool
	configDir    string
	tmpDir       string
	im           *instance.Repo
	mysqlMonitor *mock.QueryMonitor
	factory      *mock.QueryMonitorFactory
	api          *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
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

	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "im"), s.configDir, s.api)
	data, err := json.Marshal(&proto.MySQLInstance{
		Hostname: "db1",
		DSN:      "user:host@tcp:(127.0.0.1:3306)",
	})
	t.Assert(err, IsNil)
	s.im.Add("mysql", 1, data, false)

	s.mysqlMonitor = mock.NewQueryMonitor()
	s.factory = mock.NewQueryMonitorFactory(map[string]query.Monitor{
		"mysql-1": s.mysqlMonitor,
	})

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
	/**
	 * query is a proxy manager for monitors, so it's always running.
	 * It should implement the service manager interface anyway,
	 * but it doesn't actually start or stop.  Its main work is done
	 * in Handle, starting and stopping monitors (tested later).
	 */
	m := query.NewManager(s.logger, s.factory, s.im)
	t.Assert(m, Not(IsNil), Commentf("Make new query.Manager"))

	serviceInstance := proto.ServiceInstance{
		Service:    "mysql",
		InstanceId: 1,
	}

	// First the API marshals an query.Config.
	config := &query.Config{
		ServiceInstance: serviceInstance,
	}
	err := pct.Basedir.WriteConfig(query.SERVICE_NAME+"-mysql-1", config)
	t.Assert(err, IsNil)

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Its status should be "Running".
	status := m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Running")

	// Can't start manager twice.
	err = m.Start()
	t.Check(err, FitsTypeOf, pct.ServiceIsRunningError{})

	// StartService
	monitorConfig := mysqlMonitor.Config{
		Config: *config,
	}
	monitorConfigData, err := json.Marshal(monitorConfig)
	t.Assert(err, IsNil)
	// The agent calls query.Handle() with the cmd (for logging and status) and the config data.
	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "StartService",
		Data:    monitorConfigData,
	}
	reply := m.Handle(cmd)
	t.Assert(reply, NotNil)
	t.Check(reply.Error, Equals, "")

	// Explain
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
	s.mysqlMonitor.SetExplain("SELECT 1", expectedExplain)

	explainQuery := &mysql.ExplainQuery{
		ServiceInstance: serviceInstance,
		Query:           "SELECT 1",
	}
	data, err := json.Marshal(&explainQuery)
	t.Assert(err, IsNil)

	cmd = &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
		Data:    data,
	}

	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, "")

	gotExplain := &mysql.Explain{}
	err = json.Unmarshal(gotReply.Data, gotExplain)
	t.Assert(err, IsNil)
	t.Assert(gotExplain, DeepEquals, expectedExplain)

	// Stopping should be idempotent.
	err = m.Stop()
	t.Check(err, IsNil)
	err = m.Stop()
	t.Check(err, IsNil)

	status = m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Stopped")
}
