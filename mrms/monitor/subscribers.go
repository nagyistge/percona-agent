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
	"fmt"
	"sync"
	"time"

	"github.com/percona/percona-agent/pct"
)

type Subscribers struct {
	logger *pct.Logger
	// --
	subscribers       map[<-chan bool]chan bool
	globalSubscribers map[chan string]string

	sync.RWMutex
}

func NewSubscribers(logger *pct.Logger) *Subscribers {
	return &Subscribers{
		logger:            logger,
		subscribers:       make(map[<-chan bool]chan bool),
		globalSubscribers: make(map[chan string]string),
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

func (s *Subscribers) GlobalAdd(rwChan chan string, dsn string) error {
	if rwChan == nil {
		return fmt.Errorf("Invalid global channel")
	}
	if dsn == "" {
		return fmt.Errorf("DSN cannot be blank")
	}
	s.globalSubscribers[rwChan] = dsn
	return nil
}

func (s *Subscribers) GlobalRemove(inDsn string) (err error) {
	err = fmt.Errorf("Invalid DSN")
	for ch, dsn := range s.globalSubscribers {
		if dsn == inDsn {
			delete(s.globalSubscribers, ch)
			err = nil
		}
	}
	return err
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
	s.notifyGlobalSubscribers()
}

func (s *Subscribers) notifyGlobalSubscribers() {
	for globalChan, dsn := range s.globalSubscribers {
		select {
		case globalChan <- dsn:
			fmt.Println(dsn)
		case <-time.After(1 * time.Second):
			s.logger.Warn("Unable to notify global subscriber")
		}
	}
}
