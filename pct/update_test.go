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

package pct_test

import (
	"bytes"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/test"
	"github.com/percona/percona-agent/test/mock"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type UpdateTestSuite struct {
	tmpDir  string
	logChan chan *proto.LogEntry
	logger  *pct.Logger
	api     *mock.API
	pubKey  []byte
	bin     []byte
	sig     []byte
}

var _ = Suite(&UpdateTestSuite{})

func (s *UpdateTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "percona-agent-test-pct-update")
	t.Assert(err, IsNil)

	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
		"update":    "http://localhost/update",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "123", "abc-123-def", links)

	test.CopyFile(test.RootDir+"/pct/fake-percona-agent-1.0.1.go", s.tmpDir)

	cwd, err := os.Getwd()
	t.Assert(err, IsNil)
	defer os.Chdir(cwd)

	err = os.Chdir(s.tmpDir)
	t.Assert(err, IsNil)

	out, err := exec.Command("go", "build", "fake-percona-agent-1.0.1.go").Output()
	if err != nil {
		t.Logf("%s", out)
		t.Fatal(err)
	}
	s.bin, s.sig, err = test.Sign(filepath.Join(s.tmpDir, "fake-percona-agent-1.0.1"))
	t.Assert(err, IsNil)

	s.pubKey, err = ioutil.ReadFile(filepath.Join(test.RootDir, "pct", "key.pub"))
	t.Assert(err, IsNil)
}

func (s *UpdateTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *UpdateTestSuite) TestCheck(t *C) {
	// First make a very fake percona-agent "binary": just a file containing "A".
	// Later we'll check that the update process overwrites this with the updated binary.
	curBin := filepath.Join(s.tmpDir + "/percona-agent")
	if err := ioutil.WriteFile(curBin, []byte{0x41}, os.FileMode(0655)); err != nil {
		t.Fatal(err)
	}

	// Create an updater.  Normally we'd pass in hard-coded pct.PublicKey and os.Args[0].
	u := pct.NewUpdater(s.logger, s.api, s.pubKey, curBin, "1.0.0")
	t.Assert(u, NotNil)

	// Updater.Update() makes 2 API calls: first to get percona-agent-VERSION-ARCH.gz,
	// second to get the corresponding .sig file.  The bin and sig here are "real" in
	// the sense that in SetUpSuite() we compiled a fake percona-agent bin and signed
	// it with the test private key (test/pct/key.pem).
	s.api.GetCode = []int{200, 200}
	s.api.GetData = [][]byte{s.bin, s.sig}
	s.api.GetError = []error{nil, nil}

	// Run the update process.  It thinks it's getting percona-agent 1.0.1 from the real API.
	err := u.Update("1.0.1")
	t.Assert(err, IsNil)

	// The update process should have fetched and verified the test bin and sig, then
	// write the bin to disk and copied it over the very fake percona-agent "binary"
	// we created first.  So the whole process looks like:
	//   1. Test writes percona-agent (very fake binary)
	//   2. Test compiles fake-percona-agent-1.0.1.go to fake-percona-agent-1.0.1 (bin)
	//   3. Test queues bytes of fake-percona-agent-go-1.0.1 (bin) and sig in fake API
	//   4. Updater gets those ^ bytes, verifies bin with sig
	//   5. Updater writes bin bytes as percona-agent-1.0.1-ARCH
	//   6. Updater runs "percona-agent-1.0.1-ARCH -version", expects output to be "1.0.1"
	//   7. Update does "mv percona-agent-1.0.1-ARCH percona-agent"
	// Therefore, fake-percona-agent-1.0.1 (bin) == percona-agent at the byte level.
	newBin, err := ioutil.ReadFile(curBin)
	t.Assert(err, IsNil)
	if bytes.Compare(s.bin, newBin) != 0 {
		t.Error("percona-agent binary != fake-percona-agent-1.0.1 binary")
	}

	// And if percona-agent is *really* fake-percona-agent-1.0.1, then we too should
	// get "1.0.1" if we run it with -version:
	out, err := exec.Command(curBin, "-version").Output()
	t.Assert(err, IsNil)
	t.Check(strings.TrimSpace(string(out)), Equals, "percona-agent 1.0.1 rev 19b6b2ede12bfd2a012d40ac572a660be7aff1e7")
}
