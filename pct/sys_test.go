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
	"github.com/percona/percona-agent/pct"
	. "gopkg.in/check.v1"
	"os"
)

/////////////////////////////////////////////////////////////////////////////
// sys.go test suite
/////////////////////////////////////////////////////////////////////////////

type SysTestSuite struct {
}

var _ = Suite(&SysTestSuite{})

func (s *SysTestSuite) TestSameFile(t *C) {
	var err error
	var same bool

	same, err = pct.SameFile("/etc/passwd", "/etc/passwd")
	if !same {
		t.Error("/etc/passwd is same as itself")
	}
	if err != nil {
		t.Error(err)
	}

	same, err = pct.SameFile("/etc/passwd", "/etc/group")
	if same {
		t.Error("/etc/passwd is same as /etc/group")
	}
	if err != nil {
		t.Error(err)
	}

	/**
	 * Simulate renaming/rotating MySQL slow log. The original slow log is renamed,
	 * then a new slow log with the same original name is created.  These two files
	 * should _not_ be the same because they'll have different inodes.
	 */
	origFile := "/tmp/pct-test"
	newFile := "/tmp/pct-test-new"
	defer func() {
		os.Remove(origFile)
		os.Remove(newFile)
	}()

	var f1 *os.File
	f1, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f1.Close()

	os.Rename(origFile, newFile)

	var f2 *os.File
	f2, err = os.Create(origFile)
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()

	same, err = pct.SameFile(origFile, newFile)
	if same {
		t.Error(origFile, "and "+newFile+" not same after rename")
	}
	if err != nil {
		t.Error(err)
	}
}

func (s *SysTestSuite) TestMbps(t *C) {
	t.Check(pct.Mbps(0, 1.0), Equals, "0.00")
	t.Check(pct.Mbps(12749201, 0), Equals, "0.00")

	// 1 Mbps = 1048576 bytes = 8 388 608 bits = 8.39 Mbps
	t.Check(pct.Mbps(1048576, 1.0), Equals, "8.39")

	// 222566303 bytes = 1 780 530 424 bits = 1780.53 Mbps
	t.Check(pct.Mbps(222566303, 1.0), Equals, "1780.53")
	t.Check(pct.Mbps(222566303, 2.0), Equals, "890.27")
	t.Check(pct.Mbps(222566303, 300.0), Equals, "5.94")  // 5m
	t.Check(pct.Mbps(222566303, 3600.0), Equals, "0.49") // 1h
}
