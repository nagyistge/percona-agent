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

package installer_test

import (
	i "github.com/percona/percona-agent/bin/percona-agent-installer/installer"
	"github.com/percona/percona-agent/mysql"
	. "launchpad.net/gocheck"
)

type MySQLTestSuite struct {
}

var _ = Suite(&MySQLTestSuite{})

// --------------------------------------------------------------------------

func (s *MySQLTestSuite) TestMakeGrant(t *C) {
	user := "new-user"
	pass := "some pass"
	dsn := mysql.DSN{
		Username: "user",
		Password: "pass",
	}

	dsn.Hostname = "localhost"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO 'new-user'@'localhost' IDENTIFIED BY 'some pass'")

	dsn.Hostname = "127.0.0.1"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO 'new-user'@'127.0.0.1' IDENTIFIED BY 'some pass'")

	dsn.Hostname = "10.1.1.1"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO 'new-user'@'%' IDENTIFIED BY 'some pass'")

	dsn.Hostname = ""
	dsn.Socket = "/var/lib/mysql.sock"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO 'new-user'@'localhost' IDENTIFIED BY 'some pass'")
}
