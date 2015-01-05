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

package mysql_test

import (
	"fmt"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/test"
	. "gopkg.in/check.v1"
	"io/ioutil"
)

type DSNTestSuite struct {
}

var _ = Suite(&DSNTestSuite{})

func (s *DSNTestSuite) TestAllFields(t *C) {
	dsn := mysql.DSN{
		Username: "user",
		Password: "pass",
		Hostname: "host.example.com",
		Port:     "3306",
	}
	str, err := dsn.DSN()
	t.Check(err, IsNil)
	t.Check(str, Equals, "user:pass@tcp(host.example.com:3306)/?parseTime=true")

	// Stringify DSN removes password, e.g. makes it safe to print log, etc.
	str = fmt.Sprintf("%s", dsn)
	t.Check(str, Equals, "user:<password-hidden>@tcp(host.example.com:3306)")
}

func (s *DSNTestSuite) TestOldPasswords(t *C) {
	dsn := mysql.DSN{
		Username:     "user",
		Password:     "pass",
		Hostname:     "host.example.com",
		Port:         "3306",
		OldPasswords: true,
	}
	str, err := dsn.DSN()
	t.Check(err, IsNil)
	t.Check(str, Equals, "user:pass@tcp(host.example.com:3306)/?parseTime=true&allowOldPasswords=true")

	// Stringify DSN removes password, e.g. makes it safe to print log, etc.
	str = fmt.Sprintf("%s", dsn)
	t.Check(str, Equals, "user:<password-hidden>@tcp(host.example.com:3306)")
}

func (s *DSNTestSuite) TestParseSocketFromNetstat(t *C) {
	out, err := ioutil.ReadFile(test.RootDir + "/mysql/netstat001")
	t.Assert(err, IsNil)
	t.Check(mysql.ParseSocketFromNetstat(string(out)), Equals, "/var/run/mysqld/mysqld.sock")

	out, err = ioutil.ReadFile(test.RootDir + "/mysql/netstat002")
	t.Assert(err, IsNil)
	t.Check(mysql.ParseSocketFromNetstat(string(out)), Equals, "/var/lib/mysql/mysql.sock")
}

func (s *DSNTestSuite) TestHideDSNPassword(t *C) {
	dsn := "user:pass@tcp/"
	t.Check(mysql.HideDSNPassword(dsn), Equals, "user:"+mysql.HiddenPassword+"@tcp/")
	dsn = "percona-agent:0xabd123def@tcp(host.example.com:3306)/?parseTime=true"
	t.Check(mysql.HideDSNPassword(dsn), Equals, "percona-agent:"+mysql.HiddenPassword+"@tcp(host.example.com:3306)/?parseTime=true")
	dsn = ""
	t.Check(mysql.HideDSNPassword(dsn), Equals, ":"+mysql.HiddenPassword+"@")
}
