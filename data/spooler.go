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

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/pct"
	"github.com/peterbourgon/diskv"
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
	Write(tool string, data interface{}) error
	Files() <-chan string
	Read(file string) ([]byte, error)
	Remove(file string) error
	Reject(file string) error
}

// http://godoc.org/github.com/peterbourgon/diskv
type DiskvSpooler struct {
	logger   *pct.Logger
	dataDir  string
	trashDir string
	OSUUID   string
	// --
	sz           Serializer
	dataChan     chan *proto.Data
	sync         *pct.SyncChan
	cache        *diskv.Diskv
	status       *pct.Status
	mux          *sync.Mutex
	trashDataDir string
	count        uint
	size         uint64
	oldest       int64
	fileSize     map[string]int
}

func NewDiskvSpooler(logger *pct.Logger, dataDir, trashDir, OSUUID string) *DiskvSpooler {
	s := &DiskvSpooler{
		logger:   logger,
		dataDir:  dataDir,
		trashDir: trashDir,
		OSUUID:   OSUUID,
		// --
		dataChan: make(chan *proto.Data, WRITE_BUFFER),
		sync:     pct.NewSyncChan(),
		status:   pct.NewStatus([]string{"data-spooler", "data-spooler-count", "data-spooler-size", "data-spooler-oldest"}),
		mux:      new(sync.Mutex),
		fileSize: make(map[string]int),
	}
	return s
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (s *DiskvSpooler) Start(sz Serializer) error {
	s.status.Update("data-spooler", "Starting")

	// Create the data dir if necessary.  Normally the manager does this,
	// but it's necessary to create it here for testing.
	if err := pct.MakeDir(s.dataDir); err != nil {
		return err
	}

	// Create basedir/trash/data/ for Reject().
	s.trashDataDir = path.Join(s.trashDir, "data")
	if err := pct.MakeDir(s.trashDataDir); err != nil {
		return err
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

	s.mux.Lock()
	defer s.mux.Unlock()
	s.oldest = time.Now().UTC().UnixNano()
	for key := range s.cache.Keys() {
		data, err := s.cache.Read(key)
		if err != nil {
			s.logger.Error("Cannot read data file", key, ":", err)
			s.cache.Erase(key)
			continue
		}
		parts := strings.Split(key, "_") // tool_nanoUnixTs
		if len(parts) != 2 {
			s.logger.Error("Invalid data file name:", key)
			s.cache.Erase(key)
			continue
		}

		ts, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			s.logger.Error("ParseInt", key, ":", err)
			s.cache.Erase(key)
			continue
		}
		if ts < s.oldest {
			s.oldest = ts
		}
		s.count++
		s.size += uint64(len(data))
	}

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
	s.mux.Lock()
	defer s.mux.Unlock()
	s.status.Update("data-spooler-count", fmt.Sprintf("%d", s.count))
	s.status.Update("data-spooler-size", pct.Bytes(s.size))
	s.status.Update("data-spooler-oldest", fmt.Sprintf("%s", time.Unix(0, s.oldest).UTC()))
	return s.status.All()
}

func (s *DiskvSpooler) Write(tool string, data interface{}) error {
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
		OSUUID:          s.OSUUID,
		Tool:            tool,
		ContentType:     "application/json",
		ContentEncoding: s.sz.Encoding(),
		AgentVersion:    agent.VERSION,
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
	bytes, err := s.cache.Read(file)
	// Cache file size because we expect caller to call Remove() next.
	s.fileSize[file] = len(bytes)
	return bytes, err
}

func (s *DiskvSpooler) Remove(file string) error {
	size, ok := s.fileSize[file]
	if !ok {
		data, _ := s.Read(file)
		size = len(data)
	}
	// Don't lock mutex yet in case this takes awhile (it shouldn't):
	if err := s.cache.Erase(file); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	s.count--
	s.size -= uint64(size)
	if ok {
		delete(s.fileSize, file)
	}
	return nil
}

func (s *DiskvSpooler) Reject(file string) error {
	if err := os.Rename(path.Join(s.dataDir, file), path.Join(s.trashDataDir, file)); err != nil {
		return nil
	}
	// The removes the file from the cache, index, and disk, but we just
	// moved the file so removing it from disk causes a "file not found"
	// error which we can safely ignore.
	err := s.Remove(file)
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (s *DiskvSpooler) run() {
	defer func() {
		if err := recover(); err != nil {
			s.logger.Error("Data spooler crashed: ", err)
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
			ts := protoData.Created.UnixNano()
			key := fmt.Sprintf("%s_%d", protoData.Tool, ts)
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

			s.mux.Lock()
			s.count++
			s.size += uint64(len(bytes))
			if ts < s.oldest {
				s.oldest = ts
			}
			s.mux.Unlock()
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
