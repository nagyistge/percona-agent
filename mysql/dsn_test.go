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
	"fmt"
	"github.com/percona/cloud-tools/mysql"
	. "launchpad.net/gocheck"
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
	t.Check(str, Equals, "user:pass@tcp(host.example.com:3306)/")

	// Stringify DSN removes password, e.g. makes it safe to print log, etc.
	str = fmt.Sprintf("%s", dsn)
	t.Check(str, Equals, "user:...@tcp(host.example.com:3306)/")
}
