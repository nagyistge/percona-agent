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

package monitor

import (
	"github.com/percona/percona-agent/pct"
	"sync"
	"time"
)

type Subscribers struct {
	logger *pct.Logger
	// --
	subscribers map[<-chan bool]chan bool
	sync.RWMutex
}

func NewSubscribers(logger *pct.Logger) *Subscribers {
	return &Subscribers{
		logger:      logger,
		subscribers: make(map[<-chan bool]chan bool),
	}
}

func (s *Subscribers) Add() (rChan <-chan bool) {
	s.Lock()
	defer s.Unlock()

	rwChan := make(chan bool, 1)
	rChan = rwChan
	s.subscribers[rChan] = rwChan

	return rChan
}

func (s *Subscribers) Remove(rChan <-chan bool) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.subscribers[rChan]; ok {
		delete(s.subscribers, rChan)
	}
}

func (s *Subscribers) Empty() bool {
	s.RLock()
	defer s.RUnlock()

	return len(s.subscribers) == 0
}

func (s *Subscribers) Notify() {
	s.RLock()
	defer s.RUnlock()

	for _, rwChan := range s.subscribers {
		select {
		case rwChan <- true:
		case <-time.After(1 * time.Second):
			s.logger.Warn("Unable to notify subscriber")
		}
	}
}
