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
	. "launchpad.net/gocheck"
	"testing"
	"os"
	"github.com/percona/percona-agent/mysql"
	"reflect"
)

func Test(t *testing.T) { TestingT(t) }

type MysqlTestSuite struct {
	dsn string
}

var _ = Suite(&MysqlTestSuite{})

func (s *MysqlTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}
}

func (s *MysqlTestSuite) TestConnection(t *C) {
	conn := mysql.NewConnection(s.dsn)
	err := conn.Connect(1)
	t.Assert(err, IsNil)
	conn1 := reflect.ValueOf(conn.DB())

	err = conn.Connect(1)
	t.Assert(err, IsNil)
	conn2 := reflect.ValueOf(conn.DB())
	t.Check(conn1.Pointer(), Equals, conn2.Pointer())

	conn.Close()
	t.Assert(conn.DB(), NotNil)

	/**
	 * we still have open connection,
	 * because we used Connect twice,
	 * so let's close it
	 */
	conn.Close()
	t.Assert(conn.DB(), IsNil)

	// lets test accidental extra closing
	conn.Close()
	t.Assert(conn.DB(), IsNil)
}
