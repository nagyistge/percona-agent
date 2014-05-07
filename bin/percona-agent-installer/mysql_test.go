package main_test

import (
	i "github.com/percona/cloud-tools/bin/percona-agent-installer"
	"github.com/percona/cloud-tools/mysql"
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
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE ON *.* TO 'new-user'@'localhost' IDENTIFIED BY 'some pass'")

	dsn.Hostname = "127.0.0.1"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE ON *.* TO 'new-user'@'%' IDENTIFIED BY 'some pass'")

	dsn.Hostname = ""
	dsn.Socket = "/var/lib/mysql.sock"
	t.Check(i.MakeGrant(dsn, user, pass), Equals, "GRANT SUPER, PROCESS, USAGE ON *.* TO 'new-user'@'localhost' IDENTIFIED BY 'some pass'")
}
