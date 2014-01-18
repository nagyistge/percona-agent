package pct

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"sync"
)

// test/sync/WaitStatus
type StatusReporter interface {
	Status() map[string]string
}

type Status struct {
	status map[string]string
	mux    map[string]*sync.RWMutex
}

func NewStatus(procs []string) *Status {
	status := make(map[string]string)
	mux := make(map[string]*sync.RWMutex)
	for _, proc := range procs {
		status[proc] = ""
		mux[proc] = new(sync.RWMutex)
	}
	s := &Status{
		status: status,
		mux:    mux,
	}
	return s
}

func (s *Status) Update(proc string, status string) {
	if _, ok := s.status[proc]; !ok {
		return
	}
	s.mux[proc].Lock()
	defer s.mux[proc].Unlock()
	s.status[proc] = status
}

func (s *Status) UpdateRe(proc string, status string, cmd *proto.Cmd) {
	if _, ok := s.status[proc]; !ok {
		return
	}
	s.mux[proc].Lock()
	defer s.mux[proc].Unlock()
	s.status[proc] = fmt.Sprintf("%s [%s]", status, cmd)
}

func (s *Status) Get(proc string, lock bool) string {
	_, ok := s.status[proc]
	if !ok {
		return ""
	}
	if lock {
		s.mux[proc].RLock()
		defer s.mux[proc].RUnlock()
	}

	return s.status[proc]
}

func (s *Status) All() map[string]string {
	all := make(map[string]string)
	for proc, _ := range s.status {
		all[proc] = s.Get(proc, true)
	}
	return all
}

func (s *Status) Lock() {
	for _, mux := range s.mux {
		mux.RLock()
	}
}

func (s *Status) Unlock() {
	for _, mux := range s.mux {
		mux.RUnlock()
	}
}
