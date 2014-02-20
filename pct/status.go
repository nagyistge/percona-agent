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
