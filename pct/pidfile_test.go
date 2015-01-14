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

package pct_test

import (
	"github.com/percona/percona-agent/pct"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"os"
)

type TestSuite struct {
	testPidFile *pct.PidFile
	tmpFile     *os.File
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpTest(c *C) {
	s.testPidFile = pct.NewPidFile()

}

func (s *TestSuite) TestGet(t *C) {
	t.Assert(s.testPidFile.Get(), Equals, "")
}

func getTmpFileName(t *C) (string, error) {
	tmpFile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Logf("Could not get a valid tmp filename: %v", err)
		return "", err
	}
	return tmpFile.Name(), nil
}

func removeTmpFile(tmpFileName string, t *C) error {
	if err := os.Remove(tmpFileName); err != nil || !os.IsNotExist(err) {
		t.Logf("Could not delete tmp file: %v", err)
		return err
	}
	return nil
}

func (s *TestSuite) TestSet(t *C) {
	tmpFileName, err := getTmpFileName(t)
	if err != nil {
		t.Fail()
	}
	// Should fail, file already exists
	t.Assert(s.testPidFile.Set(tmpFileName), NotNil)

	// Remove the pid file from the fs
	if err := removeTmpFile(tmpFileName, t); err != nil {
		t.Fail()
	}

	t.Assert(s.testPidFile.Set(tmpFileName), Equals, nil)
	t.Assert(s.testPidFile.Get(), Equals, tmpFileName)
}

func (s *TestSuite) TestRemove(t *C) {
	tmpFileName, err := getTmpFileName(t)
	if err != nil {
		t.Fail()
	}

	if err := removeTmpFile(tmpFileName, t); err != nil {
		t.Fail()
	}

	t.Assert(s.testPidFile.Set(tmpFileName), Equals, nil)
	t.Assert(s.testPidFile.Remove(), Equals, nil)

	if tmpFile, err := os.Open(tmpFileName); err != nil && !os.IsNotExist(err) {
		t.Errorf("Failed removal of PidFile: %v", err)
		tmpFile.Close()
		removeTmpFile(tmpFileName, t)
	}

	//t.Assert(s.testPidFile.Remove(), NotNil)

}
