package data

import (
	// Core
	"os"
	"fmt"
	"time"
	"encoding/json"
	// External
	"github.com/peterbourgon/diskv"
	pct "github.com/percona/cloud-tools"
	proto "github.com/percona/cloud-protocol"
)

type Sender struct {
	logger *pct.Logger
	client proto.HttpClient
	dataChan chan interface{}
	dataDir  string
	tickerChan chan bool
	// --
	cache    *diskv.Diskv
	sync	*pct.SyncChan
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
		logger: logger,
		client: client,
		dataChan: dataChan,
		dataDir: dataDir,
		tickerChan: tickerChan,
		cache: cache,
		sync: pct.NewSyncChan(),
	}
	return s
}

func (s *Sender) Start() error {
	// Create the data dir if necessary.
	if err := os.Mkdir(s.dataDir, 0775); err != nil {
		if !os.IsExist(err) {
			// logger.Error(err)
			return err
		}
	}
	go s.spoolData()
	go s.sendData()
	return nil
}

// @goroutine
func (s *Sender) sendData() {
	// Send data at tickerChans
	client := s.client
	for _ = range s.tickerChan {

		keysChan := s.cache.Keys()
		for key := range keysChan {
			data, err := s.cache.Read(key)
			if err != nil {
				// todo
				continue
			}

			// POST the data
			err = client.Post("", data)
			if err != nil {
				// todo
			} else {
				// Remove data file after successful send.
				if err := s.cache.Erase(key); err != nil {
					// todo
					// what does this mean?  diskv can't rm the file?
				}
			}
		}
	}
}

// @goroutine
func (s *Sender) spoolData() {
	for data := range s.dataChan {
		key := fmt.Sprintf("%d", time.Now().UnixNano())
		bytes, err := json.Marshal(data)
		if err != nil {
			// todo
			s.logger.Error(err)
			continue
		}
		s.cache.Write(key, bytes)
	}
}
