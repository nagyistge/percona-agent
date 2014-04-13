package mock

import (
	"github.com/percona/cloud-tools/data"
)

type Spooler struct {
	FilesOut []string          // test provides
	DataOut  map[string][]byte // test provides
	DataIn   []interface{}
	dataChan chan interface{}
}

func NewSpooler(dataChan chan interface{}) *Spooler {
	s := &Spooler{
		dataChan: dataChan,
		DataIn:   []interface{}{},
	}
	return s
}

func (s *Spooler) Start(sz data.Serializer) error {
	return nil
}

func (s *Spooler) Stop() error {
	return nil
}

func (s *Spooler) Status() map[string]string {
	return map[string]string{"spooler": "ok"}
}

func (s *Spooler) Write(service string, data interface{}) error {
	if s.dataChan != nil {
		s.dataChan <- data
	} else {
		s.DataIn = append(s.DataIn, data)
	}
	return nil
}

func (s *Spooler) Files() <-chan string {
	filesChan := make(chan string)
	go func() {
		for _, file := range s.FilesOut {
			filesChan <- file
		}
		close(filesChan)
	}()
	return filesChan
}

func (s *Spooler) Read(file string) ([]byte, error) {
	return s.DataOut[file], nil
}

func (s *Spooler) Remove(file string) error {
	delete(s.DataOut, file)
	return nil
}
