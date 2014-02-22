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
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"math/rand"
	"time"
)

type Sender struct {
	logger     *pct.Logger
	client     pct.WebsocketClient
	spool      Spooler
	tickerChan <-chan time.Time
	// --
	sync      *pct.SyncChan
	connected bool
}

func NewSender(logger *pct.Logger, client pct.WebsocketClient, spool Spooler, tickerChan <-chan time.Time) *Sender {
	s := &Sender{
		logger:     logger,
		client:     client,
		spool:      spool,
		tickerChan: tickerChan,
		sync:       pct.NewSyncChan(),
	}
	return s
}

func (s *Sender) Start() error {
	go s.run()
	return nil
}

func (s *Sender) Stop() error {
	s.sync.Stop()
	s.sync.Wait()
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (s *Sender) run() {
	defer func() {
		if s.sync.IsGraceful() {
			s.logger.Info("Stop")
		} else {
			s.logger.Error("Crash")
		}
		s.sync.Done()
	}()

	s.logger.Info("Start")
	for {
		select {
		case <-s.tickerChan:
			s.send()
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}

func (s *Sender) send() {
	// Try a few times to connect to the API.
	connected := false
	s.logger.Debug("Connecting to API")
	for i := 1; i <= 3; i++ {
		if err := s.client.ConnectOnce(); err != nil {
			s.logger.Warn("Connect API failed:", err)
			t := int(5*rand.Float64() + 1) // [1, 5] seconds
			time.Sleep(time.Duration(t) * time.Second)
		} else {
			connected = true
			// client.WebsocketClient expects caller to recv on ConenctChan(),
			// even though in this case we're not using the async channels.
			// tood: fix this poor design assumption/coupling in ws/client.go
			defer func() {
				s.client.Disconnect()
				<-s.client.ConnectChan()
			}()
			break
		}
	}
	if !connected {
		return
	}
	s.logger.Debug("Connected to API")

	maxWarnErr := 3
	n400Err := 0
	n500Err := 0

	// Send all files.
	// todo: number/time/rate limit so we dont DDoS API
	s.logger.Info("Start sending")
	filesChan := s.spool.Files()
	for file := range filesChan {
		s.logger.Debug("Sending", file)

		data, err := s.spool.Read(file)
		if err != nil {
			s.logger.Error(err)
			continue
		}

		if err := s.client.SendBytes(data); err != nil {
			s.logger.Warn(err)
			continue
		}

		resp := &proto.Response{}
		if err := s.client.Recv(resp, 5); err != nil {
			s.logger.Warn(err)
			continue
		}

		if resp.Code >= 400 && resp.Code < 500 {
			// Something on our side is broken.
			if n400Err < maxWarnErr {
				s.logger.Warn(resp)
			}
			n400Err++
		} else if resp.Code >= 500 {
			// Something on API side is broken.
			if n500Err < maxWarnErr {
				s.logger.Warn(resp)
			}
			n500Err++
		} else {
			s.spool.Remove(file)
			s.logger.Info("Sent and removed", file)
		}
	}

	if n400Err > maxWarnErr {
		s.logger.Warn(fmt.Sprintf("%d more 4xx errors", n400Err-maxWarnErr))
	}
	if n500Err > maxWarnErr {
		s.logger.Warn(fmt.Sprintf("%d more 5xx errors", n500Err-maxWarnErr))
	}

	// todo: log some basic numbers like number of files sent, errors, time, etc.
	s.logger.Info("Done sending")
}
