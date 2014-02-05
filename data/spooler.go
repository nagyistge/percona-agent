package data

import (
	"errors"
	"fmt"
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
	Start() error
	Stop() error
	Write(data interface{}) error
	Files() <-chan string
	Read(key string) ([]byte, error)
	Remove(key string) error
}

// http://godoc.org/github.com/peterbourgon/diskv
type DiskvSpooler struct {
	logger  *pct.Logger
	dataDir string
	sz      Serializer
	// --
	dataChan chan interface{}
	sync     *pct.SyncChan
	cache    *diskv.Diskv
}

func NewDiskvSpooler(logger *pct.Logger, dataDir string, sz Serializer) *DiskvSpooler {
	s := &DiskvSpooler{
		logger:  logger,
		dataDir: dataDir,
		sz:      sz,
		// --
		dataChan: make(chan interface{}, WRITE_BUFFER),
		sync:     pct.NewSyncChan(),
	}
	return s
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (s *DiskvSpooler) Start() error {
	// Create the data dir if necessary.
	if err := os.Mkdir(s.dataDir, 0775); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

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
	return nil
}

func (s *DiskvSpooler) Write(data interface{}) error {
	select {
	case s.dataChan <- data:
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
		} else {
			s.logger.Error("spoolData crash")
		}
		s.sync.Done()
	}()

	for {
		select {
		case data := <-s.dataChan:
			bytes, err := s.sz.ToBytes(data)
			if err != nil {
				s.logger.Warn(err)
				continue
			}
			// todo: wrap in proto.Data
			key := fmt.Sprintf("%d", time.Now().UnixNano())
			if err := s.cache.Write(key, bytes); err != nil {
				s.logger.Error(err)
			}
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
