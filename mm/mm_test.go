package mm_test

import (
	"fmt"
	"os"
	"io/ioutil"
	"encoding/json"
	"time"
	"testing"
	. "launchpad.net/gocheck"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/test"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/mm"

/////////////////////////////////////////////////////////////////////////////
// Stats and Aggregator test suite
/////////////////////////////////////////////////////////////////////////////

func sendCollection(file string, collectionChan chan *mm.Collection) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	c := &mm.Collection{}
	if err = json.Unmarshal(bytes, c); err != nil {
		return err
	}
	collectionChan <- c
	return nil
}

func loadReport(file string, report *mm.Report) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(bytes, report); err != nil {
		return err
	}
	return nil
}

type AggregatorTestSuite struct{
	tickerChan chan time.Time
	collectionChan chan *mm.Collection
	reportChan chan *mm.Report
}

var _ = Suite(&AggregatorTestSuite{})

func (s *AggregatorTestSuite) SetUpSuite(t *C) {
	s.tickerChan = make(chan time.Time)
	s.collectionChan = make(chan *mm.Collection)
	s.reportChan = make(chan *mm.Report, 1)

}

func (s *AggregatorTestSuite) TestC001(t *C) {
	a := mm.NewAggregator(s.tickerChan, s.collectionChan, s.reportChan)
	go a.Run()

	if err := sendCollection(sample + "/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")

	got := test.WaitMmReport(s.reportChan)
	if got != nil {
		t.Error("No report before tick, got: %+v", got)
	}

	s.tickerChan <-t1

	got = test.WaitMmReport(s.reportChan)
	if got != nil {
		t.Error("No report after 1st tick, got: %+v", got)
	}

	if err := sendCollection(sample + "/c001.json", s.collectionChan); err != nil {
		t.Fatal(err)
	}

	s.tickerChan <-t2

	got = test.WaitMmReport(s.reportChan)
	if got == nil {
		t.Fatal("Report after 2nd tick, got: %+v", got)
	}
	if got.StartTs != t1.Unix() {
		t.Error("Report.StartTs is first Unix ts, got %s", got.StartTs)
	}
	if got.Hostname != "" {
		t.Error("Hostname not set yet, got %s", got.Hostname)
	}

	expect := &mm.Report{}
	if err := loadReport(sample + "/c001r.json", expect); err != nil {
		t.Fatal(err)
	}
	t.Assert(got.Metrics, test.DeepEquals, expect.Metrics)

	a.Sync.Stop()
	a.Sync.Wait()
}

func (s *AggregatorTestSuite) TestC002(t *C) {
	a := mm.NewAggregator(s.tickerChan, s.collectionChan, s.reportChan)
	go a.Run()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <-t1

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c002-%d.json", sample, i)
		if err := sendCollection(file, s.collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <-t2

	got := test.WaitMmReport(s.reportChan)
	expect := &mm.Report{}
	if err := loadReport(sample + "/c002r.json", expect); err != nil {
		t.Fatal("c002r.json ", err)
	}
	t.Assert(got.Metrics, test.DeepEquals, expect.Metrics)

	a.Sync.Stop()
	a.Sync.Wait()
}

func (s *AggregatorTestSuite) TestC000(t *C) {
	// collection 000 is all zero values
	a := mm.NewAggregator(s.tickerChan, s.collectionChan, s.reportChan)
	go a.Run()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	s.tickerChan <-t1

	file := sample + "/c000.json"
	if err := sendCollection(file, s.collectionChan); err != nil {
		t.Fatal(file, err)
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	s.tickerChan <-t2

	got := test.WaitMmReport(s.reportChan)
	expect := &mm.Report{}
	if err := loadReport(sample + "/c000r.json", expect); err != nil {
		t.Fatal("c000r.json ", err)
	}
	t.Assert(got.Metrics, test.DeepEquals, expect.Metrics)

	a.Sync.Stop()
	a.Sync.Wait()
}

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct{}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) TestStartService(t *C) {
}
