package mm_test

import (
	"fmt"
	"os"
	"io/ioutil"
	"encoding/json"
	"time"
	"testing"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/test"
	"github.com/percona/cloud-tools/test/mock"
)

var sample = os.Getenv("GOPATH") + "/src/github.com/percona/cloud-tools/test/mm"

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

var tickerChan = make(chan time.Time)
var ticker = mock.NewTicker(nil, tickerChan)
var collectionChan = make(chan *mm.Collection)
var dataChan = make(chan interface{}, 1)

func TestC001(t *testing.T) {
	a := mm.NewAggregator(ticker, collectionChan, dataChan)
	go a.Start()
	defer a.Stop()

	// Send load collection from file and send to aggregator.
	if err := sendCollection(sample + "/c001.json", collectionChan); err != nil {
		t.Fatal(err)
	}

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")

	got := test.WaitMmReport(dataChan)
	if got != nil {
		t.Error("No report before tick, got: %+v", got)
	}

	tickerChan <-t1

	got = test.WaitMmReport(dataChan)
	if got != nil {
		t.Error("No report after 1st tick, got: %+v", got)
	}

	if err := sendCollection(sample + "/c001.json", collectionChan); err != nil {
		t.Fatal(err)
	}

	tickerChan <-t2

	got = test.WaitMmReport(dataChan)
	if got == nil {
		t.Fatal("Report after 2nd tick, got: %+v", got)
	}
	if got.StartTs != t1.Unix() {
		t.Error("Report.StartTs is first Unix ts, got %s", got.StartTs)
	}

	expect := &mm.Report{}
	if err := loadReport(sample + "/c001r.json", expect); err != nil {
		t.Fatal(err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

func TestC002(t *testing.T) {
	a := mm.NewAggregator(ticker, collectionChan, dataChan)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	tickerChan <-t1

	for i := 1; i <= 5; i++ {
		file := fmt.Sprintf("%s/c002-%d.json", sample, i)
		if err := sendCollection(file, collectionChan); err != nil {
			t.Fatal(file, err)
		}
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	tickerChan <-t2

	got := test.WaitMmReport(dataChan)
	expect := &mm.Report{}
	if err := loadReport(sample + "/c002r.json", expect); err != nil {
		t.Fatal("c002r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}

func TestC000(t *testing.T) {
	a := mm.NewAggregator(ticker, collectionChan, dataChan)
	go a.Start()
	defer a.Stop()

	t1, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:00:00 -0700 MST 2014")
	tickerChan <-t1

	// collection 000 is all zero values
	file := sample + "/c000.json"
	if err := sendCollection(file, collectionChan); err != nil {
		t.Fatal(file, err)
	}

	t2, _ := time.Parse("Jan 2 15:04:05 -0700 MST 2006", "Jan 1 12:05:00 -0700 MST 2014")
	tickerChan <-t2

	got := test.WaitMmReport(dataChan)
	expect := &mm.Report{}
	if err := loadReport(sample + "/c000r.json", expect); err != nil {
		t.Fatal("c000r.json ", err)
	}
	if ok, diff := test.IsDeeply(got.Metrics, expect.Metrics); !ok {
		t.Fatal(diff)
	}
}
