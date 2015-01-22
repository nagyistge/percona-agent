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

package install

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/percona/percona-agent/pct"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type MainTestSuite struct {
	cmd        *exec.Cmd
	basedir    string
	bin        string
	anotherbin string
	initscript string
	username   string
}

const (
	MOCKED_PERCONA_AGENT = "github.com/percona/percona-agent/install/mock"
)

var _ = Suite(&MainTestSuite{})

func (s *MainTestSuite) SetUpSuite(t *C) {
	var err error

	// We can't/shouldn't use /usr/local/percona/ (the default basedir),
	// so use a tmpdir instead with only a bin dir inside
	s.basedir, err = ioutil.TempDir("/tmp", "percona-agent-init-test-")
	t.Assert(err, IsNil)
	err = os.Mkdir(filepath.Join(s.basedir, pct.BIN_DIR), 0777)
	t.Assert(err, IsNil)

	// Lets compile and place the mocked percona-agent on the tmp basedir
	s.bin = filepath.Join(s.basedir, pct.BIN_DIR, "percona-agent")
	cmd := exec.Command("go", "build", "-o", s.bin, MOCKED_PERCONA_AGENT)
	err = cmd.Run()
	// Failed to compile mocked percona-agent
	t.Assert(err, IsNil, Commentf("Failed to build mocked percona agent: %v", err))

	// Get current username to set test env variable
	user, erruser := user.Current()
	t.Assert(erruser, IsNil, Commentf("Failed to obtain current user: %v", err))
	s.username = user.Username

	// Copy init script to tmp basedir/bin directory
	initscript, err := filepath.Abs("./percona-agent")
	// Check if absolute path resolving succedeed
	t.Assert(err, IsNil)
	// Check if init script is there
	t.Assert(pct.FileExists(initscript), Equals, true)
	s.initscript = filepath.Join(s.basedir, pct.BIN_DIR, "init-script")
	cmd = exec.Command("cp", initscript, s.initscript)
	err = cmd.Run()
	t.Assert(err, IsNil, Commentf("Failed to copy init script to tmp dir: %v", err))

	// Set all env vars to default test values
	resetTestEnvVars(s)
}

func (s *MainTestSuite) TearDownTest(t *C) {
	// Delete any left pid file and set mocked agent start delay to 0
	resetTestEnvVars(s)
	// Kill any remaining process before deleting pidfile
	if pid, err := readPidFile(filepath.Join(s.basedir, "percona-agent.pid")); pid != "" && err == nil {
		if numPid, err := strconv.ParseInt(pid, 10, 0); err == nil {
			syscall.Kill(int(numPid), syscall.SIGTERM)
		}
	}
	// Delete if pidFile exists
	os.Remove(filepath.Join(s.basedir, "percona-agent.pid"))
}

func (s *MainTestSuite) TearDownSuite(t *C) {
	// Delete tmp
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

func resetTestEnvVars(s *MainTestSuite) {
	// Sadly no os.Unsetenv in Go 1.3.x
	os.Setenv("TEST_PERCONA_AGENT_START_DELAY", "")
	os.Setenv("TEST_PERCONA_AGENT_STOP_DELAY", "")
	os.Setenv("PERCONA_AGENT_START_TIMEOUT", "")
	os.Setenv("PERCONA_AGENT_STOP_TIMEOUT", "")
	os.Setenv("PERCONA_AGENT_USER", s.username)
	os.Setenv("PERCONA_AGENT_DIR", s.basedir)
}

func writePidFile(filePath, pid string) error {
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		//Could not create pidfile
		return err
	}
	// Write PID to file
	if _, err := file.WriteString(pid); err != nil {
		// Could not write to stale pidfile
		return err
	}
	file.Close()
	return nil
}

func readPidFile(pidFilePath string) (pid string, err error) {
	if bytes, err := ioutil.ReadFile(pidFilePath); err != nil {
		return "", err
	} else {
		// Remove any \n
		return strings.Replace(string(bytes), "\n", "", -1), nil
	}
}

//-----------------------------------------------------------------------------

func (s *MainTestSuite) TestStatusNoAgent(t *C) {
	cmd := exec.Command(s.initscript, "status")
	output, err := cmd.Output()
	// status exit code should be 1
	t.Check(err, NotNil)
	// script should output a message
	t.Assert(string(output), Equals, "percona-agent is not running.\n")
}

func (s *MainTestSuite) TestStopNoAgent(t *C) {
	cmd := exec.Command(s.initscript, "stop")
	output, err := cmd.Output()
	// stop exit code should be 0
	t.Check(err, IsNil)
	// script should output a message
	t.Assert(string(output), Equals, "Stopping percona-agent...\npercona-agent is not running.\n")
}

func (s *MainTestSuite) TestStartStop(t *C) {
	// Start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// Start exit code should be 0
	t.Check(err, IsNil)
	// Script should output a message
	t.Assert(string(output), Equals, "Starting percona-agent...\nWaiting for percona-agent to start...\nOK\n")

	// Check status
	cmd = exec.Command(s.initscript, "status")
	output, err = cmd.Output()
	// Status exit code should be 0
	t.Check(err, IsNil)
	// Extract PID from command output
	rePID := regexp.MustCompile(`^percona-agent\ is\ running\ \((\d+)\)\.`)
	found := rePID.FindStringSubmatch(string(output))
	// Check if the command provided a PID
	var pid string
	if len(found) == 2 {
		pid = found[1]
	} else {
		t.Error("Could not get pid for mocked percona-agent")
	}

	pidbinary, err := os.Readlink(fmt.Sprintf("/proc/%v/exe", pid))
	// Check that PID actually points to our mocked percona-agent binary
	t.Assert(pidbinary, Equals, s.bin)

	// Now try to stop
	cmd = exec.Command(s.initscript, "stop")
	output, err = cmd.Output()
	// stop exit code should be 0
	t.Check(err, IsNil)
	// script should output a message
	t.Assert(string(output), Equals, "Stopping percona-agent...\nWaiting for percona-agent to exit...\nStopped percona-agent.\n")
}

func (s *MainTestSuite) TestDoubleStart(t *C) {
	// Start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)
	// Script should output a message
	t.Assert(string(output), Equals, "Starting percona-agent...\nWaiting for percona-agent to start...\nOK\n")

	// Start service again
	cmd = exec.Command(s.initscript, "start")
	output, err = cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)
	// script should output a message
	t.Assert(string(output), Equals, "Starting percona-agent...\npercona-agent is already running.\n")
}

func (s *MainTestSuite) TestWrongBin(t *C) {
	pidFilePath := filepath.Join(s.basedir, "percona-agent.pid")
	// Create pidfile with valid PID but not corresponding to a mocked percona-agent
	if err := writePidFile(pidFilePath, string(os.Getpid())); err != nil {
		t.Errorf("Could not create pidfile: %v", err)
	}

	// Now start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)
	// Script should output a message
	t.Assert(string(output), Equals, fmt.Sprintf("Starting percona-agent...\nRemoved stale pid file: %v\nWaiting for "+
		"percona-agent to start...\nOK\n", pidFilePath))
}

func (s *MainTestSuite) TestStalePIDFile(t *C) {
	// Create pidfile with non valid PID
	pidFilePath := filepath.Join(s.basedir, "percona-agent.pid")
	if err := writePidFile(pidFilePath, string(rand.Uint32())); err != nil {
		t.Errorf("Could not create pidfile: %v", err)
	}

	// Now start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)
	// script should output a message
	t.Assert(string(output), Equals, fmt.Sprintf("Starting percona-agent...\nRemoved stale pid file: %v\nWaiting for "+
		"percona-agent to start...\nOK\n", pidFilePath))
}

func (s *MainTestSuite) TestEnvVariables(t *C) {
	// Set percona-agent user to run with nobody
	os.Setenv("PERCONA_AGENT_USER", "nobody")
	// Now start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 1
	t.Check(err, NotNil)
	// script should output no message TODO: check this
	t.Check(string(output), Equals, "")

	// Set to percona-agent basedir to non existant directory
	os.Setenv("PERCONA_AGENT_DIR", filepath.Join(s.basedir, string(rand.Uint32())))
	// Try to start service
	cmd = exec.Command(s.initscript, "start")
	output, err = cmd.Output()
	// start exit code should be 1
	t.Check(err, NotNil)
	// script should output no message TODO: check this
	t.Check(string(output), Equals, "")
}

func (s *MainTestSuite) TestDelayedStart(t *C) {
	// Set init script timeout to 1 second
	os.Setenv("PERCONA_AGENT_START_TIMEOUT", "1")
	// Set percona-agent start delay to 2 seconds
	os.Setenv("TEST_PERCONA_AGENT_START_DELAY", "2")
	// Now try to start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 1
	t.Check(err, NotNil)
	// path to log file, its part of the output
	perconaLogPath := filepath.Join(s.basedir, "percona-agent.log")
	// script should output message
	t.Check(string(output), Equals, fmt.Sprintf("Starting percona-agent...\nWaiting for percona-agent to start...\nFail.  "+
		"Check %v for details.\n", perconaLogPath))
}

func (s *MainTestSuite) TestDelayedStop(t *C) {
	// Set init script stop timeout to 1 second
	os.Setenv("PERCONA_AGENT_STOP_TIMEOUT", "1")
	// Set percona-agent stop delay to 2 seconds
	os.Setenv("TEST_PERCONA_AGENT_STOP_DELAY", "2")
	// Now try to start service
	cmd := exec.Command(s.initscript, "start")
	output, err := cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)

	// Get the PID from the pidfile
	pid, err := readPidFile(filepath.Join(s.basedir, "percona-agent.pid"))
	// Check if we could read the pidfile
	t.Check(err, IsNil)
	// pid should be non empty
	t.Check(pid, Not(Equals), "")

	stop_cmd := exec.Command(s.initscript, "stop")
	output, err = stop_cmd.Output()
	// start exit code should be 0
	t.Check(err, IsNil)

	// Script should output message
	t.Check(string(output), Equals, fmt.Sprintf("Stopping percona-agent...\nWaiting for percona-agent to exit...\n"+
		"Time out waiting for percona-agent to exit.  Trying kill -9 %v...\nStopped percona-agent.\n", pid))
	// Make sure the process was killed
	t.Assert(pct.FileExists(fmt.Sprintf("/proc/%v/stat", pid)), Equals, false)
}
