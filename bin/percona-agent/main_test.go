/*
   Copyright (c) 2015, Percona LLC and/or its affiliates. All rights reserved.

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

package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	username    string
	basedir     string
	bin         string
	cmd         *exec.Cmd
	agentconfig string
}

var _ = Suite(&MainTestSuite{})

func (s *MainTestSuite) SetUpSuite(t *C) {
	var err error

	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	s.basedir, err = ioutil.TempDir("/tmp", "percona-agent-test-")
	t.Assert(err, IsNil)
	err = os.Mkdir(path.Join(s.basedir, pct.BIN_DIR), 0777)
	t.Assert(err, IsNil)
	err = os.Mkdir(path.Join(s.basedir, pct.CONFIG_DIR), 0777)
	t.Assert(err, IsNil)

	s.bin = s.basedir + "/percona-agent"
	cmd := exec.Command("go", "build", "-o", s.bin)
	err = cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to build test percona-agent: %s", err))
	testAgentConfig := path.Join(test.RootDir, "/agent/config001.json")
	s.agentconfig = path.Join(s.basedir, pct.CONFIG_DIR, "agent.conf")
	cmd = exec.Command("cp", testAgentConfig, s.agentconfig)
	err = cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to copy agent config to tmp basedir: %v", err))
}

func (s *MainTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

func (s *MainTestSuite) SetUpTest(t *C) {
	s.cmd = nil
}

func (s *MainTestSuite) TearDownTest(t *C) {
	// Kill running test process (if any)
	s.cmd.Process.Kill()
	// Delete pidFile if any at all
	os.Remove(filepath.Join(s.basedir, "percona-agent.pid"))
}

func startWaitIsAlive(s *MainTestSuite, t *C) {
	t.Assert(s.cmd.Start(), IsNil)
	// Lets give the process enough time to start (or die) and then check if its alive
	time.Sleep(200 * time.Millisecond)
	err := s.cmd.Process.Signal(syscall.Signal(0))
	// test the existence of process in the traditional unix way
	t.Assert(err, IsNil)
}

//-----------------------------------------------------------------------------

func (s *MainTestSuite) TestInvalidPossitional(t *C) {
	s.cmd = exec.Command(s.bin, "-basedir="+s.basedir, "nope")
	// percona-agent should not accept possitional cmd line arguments
	t.Assert(s.cmd.Run(), NotNil)
}

func (s *MainTestSuite) TestInvalidFlag(t *C) {
	s.cmd = exec.Command(s.bin, "-basedir="+s.basedir, "-nope")
	// percona-agent should not accept to start with invalid flag
	t.Assert(s.cmd.Run(), NotNil)
}

func (s *MainTestSuite) TestNoExtraFlag(t *C) {
	s.cmd = exec.Command(s.bin, "-basedir="+s.basedir)
	startWaitIsAlive(s, t)
}

func (s *MainTestSuite) TestValidFlag(t *C) {
	s.cmd = exec.Command(s.bin, "-basedir="+s.basedir, "-ping=false")
	startWaitIsAlive(s, t)
}
