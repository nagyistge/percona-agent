package data

import (
	// Core
	"encoding/json"
	"fmt"
	"os"
	"time"
	// External
	proto "github.com/percona/cloud-protocol"
	pct "github.com/percona/cloud-tools"
	"github.com/peterbourgon/diskv"
)

type Sender struct {
	logger     *pct.Logger
	client     proto.HttpClient
	dataChan   chan interface{}
	dataDir    string
	tickerChan chan bool
	// --
	cache *diskv.Diskv
	sync  *pct.SyncChan
}

func NewSender(logger *pct.Logger, client proto.HttpClient, dataChan chan interface{}, dataDir string, tickerChan chan bool) *Sender {
	cache := diskv.New(diskv.Options{
		BasePath:     dataDir,
		Transform:    func(s string) []string { return []string{} },
		CacheSizeMax: 1024 * 1024,
		Index:        &diskv.LLRBIndex{},
		IndexLess:    func(a, b string) bool { return a < b },
	})

	s := &Sender{
		logger:     logger,
		client:     client,
		dataChan:   dataChan,
		dataDir:    dataDir,
		tickerChan: tickerChan,
		cache:      cache,
		sync:       pct.NewSyncChan(),
	}
	return s
}

func (s *Sender) Start() error {
	// Create the data dir if necessary.
	if err := os.Mkdir(s.dataDir, 0775); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	go s.spoolData()
	go s.sendData()
	// To stop, close tickerChan which will stop sendData.
	// When sendData returns, it stops spoolData.
	return nil
}

// @goroutine
func (s *Sender) sendData() {
	defer func() {
		if s.sync.IsGraceful() {
			s.logger.Info("sendData stop")
			s.sync.Stop()
		} else {
			s.logger.Error("sendData crash")
			// Let spoolData keep running even if we crash because it's good
			// to keep spooling data so the tools can keep running.
		}
	}()

	for _ = range s.tickerChan {
		keysChan := s.cache.Keys()
		for key := range keysChan {
			data, err := s.cache.Read(key)
			if err != nil {
				s.logger.Error(err)
				continue
			}

			// POST the data
			err = s.client.Post("", data)
			if err != nil {
				s.logger.Error(err)
			} else {
				// Remove data file after successful send.
				if err := s.cache.Erase(key); err != nil {
					// what does this mean?  diskv can't rm the file?
					s.logger.Error(err)
				}
			}
		}
	}
	s.sync.Graceful()
}

// @goroutine
func (s *Sender) spoolData() {
	defer func() {
		if s.sync.IsGraceful() {
			s.logger.Info("spoolData stop")
		} else {
			s.logger.Error("spoolData crash")
		}
	}()

	for {
		select {
		case data := <-s.dataChan:
			key := fmt.Sprintf("%d", time.Now().UnixNano())
			bytes, err := json.Marshal(data)
			if err != nil {
				s.logger.Error(err)
			} else {
				if err := s.cache.Write(key, bytes); err != nil {
					s.logger.Error(err)
				}
			}
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
