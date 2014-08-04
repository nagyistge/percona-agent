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

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"github.com/peterbourgon/diskv"
	"os"
	"sync"
	"time"
)

const (
	WRITE_BUFFER = 100
	CACHE_SIZE   = 1024 * 1024 * 8 // 8M
)

var ErrSpoolTimeout = errors.New("Timeout spooling data")

type Spooler interface {
	Start(Serializer) error
	Stop() error
	Status() map[string]string
	Write(service string, data interface{}) error
	Files() <-chan string
	Read(key string) ([]byte, error)
	Remove(key string) error
}

// http://godoc.org/github.com/peterbourgon/diskv
type DiskvSpooler struct {
	logger   *pct.Logger
	dataDir  string
	hostname string
	// --
	sz       Serializer
	dataChan chan *proto.Data
	sync     *pct.SyncChan
	cache    *diskv.Diskv
	status   *pct.Status
	mux      *sync.Mutex
}

func NewDiskvSpooler(logger *pct.Logger, dataDir string, hostname string) *DiskvSpooler {
	s := &DiskvSpooler{
		logger:   logger,
		dataDir:  dataDir,
		hostname: hostname,
		// --
		dataChan: make(chan *proto.Data, WRITE_BUFFER),
		sync:     pct.NewSyncChan(),
		status:   pct.NewStatus([]string{"data-spooler"}),
		mux:      new(sync.Mutex),
	}
	return s
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (s *DiskvSpooler) Start(sz Serializer) error {
	// Create the data dir if necessary.
	if err := os.Mkdir(s.dataDir, 0775); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	// T{} -> []byte
	s.sz = sz

	// diskv reads all files in BasePath on startup.
	s.cache = diskv.New(diskv.Options{
		BasePath:     s.dataDir,
		Transform:    func(s string) []string { return []string{} },
		CacheSizeMax: CACHE_SIZE,
		Index:        &diskv.LLRBIndex{},
		IndexLess:    func(a, b string) bool { return a < b },
	})

	go s.run()
	s.logger.Info("Started")
	return nil
}

func (s *DiskvSpooler) Stop() error {
	s.sync.Stop()
	s.sync.Wait()
	s.sz = nil
	s.cache = nil
	s.logger.Info("Stopped")
	return nil
}

func (s *DiskvSpooler) Status() map[string]string {
	return s.status.All()
}

func (s *DiskvSpooler) Write(service string, data interface{}) error {
	/**
	 * This method is shared: multiple goroutines call it to write data.
	 * If the data serializer (sz) is not concurrent, then we serialize
	 * access.  For example, the JSON text sz is concurrent, but the gzip
	 * sz is not because it uses internal, non-mutex-guarded buffers.
	 */
	if !s.sz.Concurrent() {
		s.mux.Lock()
		defer s.mux.Unlock()
	}

	s.logger.Debug("write:call")
	defer s.logger.Debug("write:return")

	// Serialize the data: T{} -> []byte
	encodedData, err := s.sz.ToBytes(data)
	if err != nil {
		return err
	}

	// Wrap data in proto.Data with metadata to allow API to handle it properly.
	protoData := &proto.Data{
		Created:         time.Now().UTC(),
		Hostname:        s.hostname,
		Service:         service,
		ContentType:     "application/json",
		ContentEncoding: s.sz.Encoding(),
		Data:            encodedData,
	}

	// Write data to disk.
	select {
	case s.dataChan <- protoData:
	case <-time.After(100 * time.Millisecond):
		// Let caller decide what to do.
		s.logger.Debug("write:timeout")
		return ErrSpoolTimeout
	}

	return nil
}

func (s *DiskvSpooler) Files() <-chan string {
	return s.cache.Keys()
}

func (s *DiskvSpooler) Read(file string) ([]byte, error) {
	return s.cache.Read(file)
}

func (s *DiskvSpooler) Remove(file string) error {
	return s.cache.Erase(file)
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (s *DiskvSpooler) run() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Recovered while running spooler: ", r)
		}
		if s.sync.IsGraceful() {
			s.logger.Info("spoolData stop")
			s.status.Update("data-spooler", "Stopped")
		} else {
			s.logger.Error("spoolData crash")
			s.status.Update("data-spooler", "Crashed")
		}
		s.sync.Done()
	}()

	for {
		s.status.Update("data-spooler", "Idle")
		select {
		case protoData := <-s.dataChan:
			key := fmt.Sprintf("%s_%d", protoData.Service, protoData.Created.UnixNano())
			s.logger.Debug("run:spool:" + key)
			s.status.Update("data-spooler", "Spooling "+key)

			bytes, err := json.Marshal(protoData)
			if err != nil {
				s.logger.Error(err)
				continue
			}

			if err := s.cache.Write(key, bytes); err != nil {
				s.logger.Error(err)
			}
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
