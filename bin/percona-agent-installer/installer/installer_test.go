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

package installer_test

import (
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/bin/percona-agent-installer/api"
	"github.com/percona/percona-agent/bin/percona-agent-installer/installer"
	"github.com/percona/percona-agent/bin/percona-agent-installer/term"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test/mock"
	. "gopkg.in/check.v1"
	"os"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type InstallerTestSuite struct{}

var _ = Suite(&InstallerTestSuite{})

func (i *InstallerTestSuite) TestIsSupportedMySQLVersion(t *C) {
	agentConfig := &agent.Config{}
	flags := installer.Flags{}

	api := api.New(pct.NewAPI(), false)
	terminal := term.NewTerminal(os.Stdin, false, true)
	inst := installer.NewInstaller(terminal, "", api, agentConfig, flags)
	conn := mock.NewNullMySQL()

	conn.SetGlobalVarString("version", "5.0") // Mockup MySQL version
	got, err := inst.IsVersionSupported(conn)
	t.Assert(err, IsNil)
	t.Assert(got, Equals, false) // Agent doesn't support MySQL 5.0

	conn.SetGlobalVarString("version", "ubuntu-something") // Malformed version
	got, err = inst.IsVersionSupported(conn)
	t.Assert(err, NotNil)
	t.Assert(got, Equals, false)

	conn.SetGlobalVarString("version", "5.0.1-ubuntu-something")
	got, err = inst.IsVersionSupported(conn)
	t.Assert(err, IsNil)
	t.Assert(got, Equals, false)

	conn.SetGlobalVarString("version", "5.1.0-ubuntu-something")
	got, err = inst.IsVersionSupported(conn)
	t.Assert(err, IsNil)
	t.Assert(got, Equals, true)

	conn.SetGlobalVarString("version", "10.1.0-MariaDB")
	got, err = inst.IsVersionSupported(conn)
	t.Assert(err, IsNil)
	t.Assert(got, Equals, true)

	conn.Close()
}
