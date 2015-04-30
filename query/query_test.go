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

package query_test

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/query"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, query.SERVICE_NAME+"-manager-test")
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStopHandleManager(t *C) {
	var err error

	// Create explain service
	explainService := mock.NewQueryService()

	// Create query manager
	m := query.NewManager(s.logger, explainService)
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

	// Test known cmd
	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
	}
	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, "")

	// Test unknown cmd
	cmd = &proto.Cmd{
		Service: "query",
		Cmd:     "Unknown",
	}
	gotReply = m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, fmt.Sprintf("Unknown command: %s", cmd.Cmd))

	// You can't stop this service
	err = m.Stop()
	t.Check(err, IsNil)
	status = m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Running")
}
