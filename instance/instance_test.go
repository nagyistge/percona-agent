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

package instance_test

import (
	//"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/sid"
	"github.com/percona/cloud-tools/test"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"os"
	"path/filepath"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type SidTestSuite struct {
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	configDir string
}

var _ = Suite(&SidTestSuite{})

func (s *SidTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "sid_test")

	dir, _ := ioutil.TempDir("/tmp", "pct-sid-test")
	s.configDir = dir
}

func (s *SidTestSuite) SetUpTest(t *C) {
	files, _ := filepath.Glob(s.configDir + "/*")
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Error(err)
		}
	}
}

func (s *SidTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.configDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *SidTestSuite) TestFoo(t *C) {
}
