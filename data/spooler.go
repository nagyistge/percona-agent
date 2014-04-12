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
	"github.com/percona/cloud-tools/pct"
	"github.com/peterbourgon/diskv"
	"os"
	"time"
)

const (
	WRITE_BUFFER = 100
	CACHE_SIZE   = 1024 * 1024 * 8 // 8M
)

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
	return nil
}

func (s *DiskvSpooler) Stop() error {
	s.sync.Stop()
	s.sync.Wait()
	s.sz = nil
	s.cache = nil
	return nil
}

func (s *DiskvSpooler) Status() map[string]string {
	return s.status.All()
}

func (s *DiskvSpooler) Write(service string, data interface{}) error {
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
	default:
		s.logger.Warn("Spool write buffer is full")
		return errors.New("Spool write buffer is full")
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
			s.status.Update("data-spooler", "Spooling data")
			key := fmt.Sprintf("%s_%d", protoData.Service, protoData.Created.UnixNano())

			bytes, err := json.Marshal(protoData)
			if err != nil {
				s.logger.Error(err)
				continue
			}

			if err := s.cache.Write(key, bytes); err != nil {
				s.logger.Error(err)
			}

			s.logger.Debug("Spooled ", key)
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
