package pct_test

import (
	"github.com/percona/cloud-tools/pct"
	"os"
	"testing"
	"time"
)

/////////////////////////////////////////////////////////////////////////////
// sys.go test suite
/////////////////////////////////////////////////////////////////////////////

func TestSameFile(t *testing.T) {
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
