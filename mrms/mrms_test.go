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

package mrms_test

import (
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mrms"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test/mock"
	. "launchpad.net/gocheck"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Test Suite
/////////////////////////////////////////////////////////////////////////////

type TestSuite struct {
	mockMrmsMonitor *mock.MrmsMonitor
	logChan         chan *proto.LogEntry
	logger          *pct.Logger
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.mockMrmsMonitor = mock.NewMrmsMonitor()
	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "mrms-test")
}

func (s *TestSuite) TestStartStop(t *C) {
	var err error
	m := mrms.NewManager(s.logger, s.mockMrmsMonitor)

	/**
	 * Start MRMS
	 */
	err = m.Start()
	t.Assert(err, IsNil)

	/**
	 * Stop MRMS
	 */
	err = m.Stop()
	t.Assert(err, IsNil)
}
