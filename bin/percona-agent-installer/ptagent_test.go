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

package main_test

import (
	"github.com/percona/percona-agent/agent"
	i "github.com/percona/percona-agent/bin/percona-agent-installer"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/test"
	"io/ioutil"
	. "launchpad.net/gocheck"
)

type PTAgentTestSuite struct {
}

var _ = Suite(&PTAgentTestSuite{})

// --------------------------------------------------------------------------

func (s *PTAgentTestSuite) TestParsePTAgentConf001(t *C) {
	content, err := ioutil.ReadFile(test.RootDir + "/installer/pt-agent.conf-001")
	t.Assert(err, IsNil)
	agent := &agent.Config{}
	dsn := &mysql.DSN{}
	libDir, err := i.ParsePTAgentConf(string(content), agent, dsn)
	t.Assert(err, IsNil)
	t.Check(libDir, Equals, "/var/lib/pt-agent")
	t.Check(agent.ApiKey, Equals, "10000000000000000000000000000001")
	t.Check(dsn.Socket, Equals, "/var/lib/mysql/mysql.sock")
}

func (s *PTAgentTestSuite) TestParseMyCnf001(t *C) {
	content, err := ioutil.ReadFile(test.RootDir + "/installer/my.cnf-001")
	t.Assert(err, IsNil)
	dsn := &mysql.DSN{}
	err = i.ParseMyCnf(string(content), dsn)
	t.Assert(err, IsNil)
	t.Check(dsn.Username, Equals, "pt_agent")
	t.Check(dsn.Password, Equals, "foobar")
}

func (s *PTAgentTestSuite) TestParsePTAgentResource(t *C) {
	content, err := ioutil.ReadFile(test.RootDir + "/installer/agent-001")
	t.Assert(err, IsNil)
	agent := &agent.Config{}
	err = i.ParsePTAgentResource(content, agent)
	t.Assert(err, IsNil)
	t.Check(agent.AgentUuid, Equals, "00000000-0000-0000-0000-000000000001")
}
