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
	"fmt"
	"github.com/percona/percona-agent/pct"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
)

type TestSuite struct {
	basedir     string
	testPidFile *pct.PidFile
	tmpFile     *os.File
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	basedir, err := ioutil.TempDir("", "pidfile-test-")
	s.basedir = basedir
	t.Assert(err, IsNil)
	if err := pct.Basedir.Init(s.basedir); err != nil {
		t.Errorf("Could initialize tmp Basedir: %v", err)
	}
}

func (s *TestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.basedir); err != nil {
		t.Error(err)
	}
}

func (s *TestSuite) SetUpTest(t *C) {
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
	if err := os.Remove(tmpFileName); err != nil {
		t.Logf("Could not delete tmp file: %v", err)
		return err
	}
	return nil
}

func getRandFilename() string {
	return fmt.Sprintf("%v.pid", rand.Int())
}

func openCloseFile(tmpFileName string, t *C) error {
	tmpFile, err := os.Open(tmpFileName)
	if err != nil && os.IsNotExist(err) {
		t.Logf("PidFile not found")
		return err
	} else if err != nil {
		t.Logf("Could not open PidFile: %v", err)
		return err
	}
	tmpFile.Close()
	return nil
}

func (s *TestSuite) TestSet(t *C) {
	t.Assert(s.testPidFile.Set(""), Equals, nil)

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
	defer os.Remove(tmpFileName)

	if err := openCloseFile(tmpFileName, t); err != nil {
		t.Fail()
	}

	randPidFileName := getRandFilename()
	t.Assert(s.testPidFile.Set(randPidFileName), Equals, nil)
	fullPath := filepath.Join(pct.Basedir.Path(), randPidFileName)
	defer os.Remove(fullPath)
}

func (s *TestSuite) TestRemove(t *C) {
	t.Assert(s.testPidFile.Set(""), Equals, nil)
	t.Assert(s.testPidFile.Remove(), Equals, nil)

	tmpFilePath, err := getTmpFileName(t)
	if err != nil {
		t.Fail()
	}
	// We have a valid tmp file name now, remove it
	t.Assert(removeTmpFile(tmpFilePath, t), Equals, nil)

	t.Assert(s.testPidFile.Set(tmpFilePath), Equals, nil)
	t.Assert(s.testPidFile.Remove(), Equals, nil)
	_, err = os.Open(tmpFilePath)
	t.Assert(err, NotNil)

	t.Assert(s.testPidFile.Set(tmpFilePath), Equals, nil)
	t.Assert(removeTmpFile(tmpFilePath, t), Equals, nil)
	t.Assert(s.testPidFile.Remove(), Equals, nil)

	t.Assert(s.testPidFile.Set(tmpFilePath), Equals, nil)
	t.Assert(removeTmpFile(tmpFilePath, t), Equals, nil)
	t.Assert(s.testPidFile.Remove(), Equals, nil)
	if tmpFile, err := os.Open(tmpFilePath); err != nil && !os.IsNotExist(err) {
		tmpFile.Close()
		defer removeTmpFile(tmpFilePath, t)
		t.Errorf("Failed removal of PidFile: %v", err)
	}

}
