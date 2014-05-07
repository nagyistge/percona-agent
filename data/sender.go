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
	"github.com/percona/percona-agent/pct"
	"math/rand"
	"time"
)

type Sender struct {
	logger *pct.Logger
	client pct.WebsocketClient
	// --
	spool      Spooler
	tickerChan <-chan time.Time
	blackhole  bool
	sync       *pct.SyncChan
	status     *pct.Status
}

func NewSender(logger *pct.Logger, client pct.WebsocketClient) *Sender {
	s := &Sender{
		logger: logger,
		client: client,
		sync:   pct.NewSyncChan(),
		status: pct.NewStatus([]string{"data-sender"}),
	}
	return s
}

func (s *Sender) Start(spool Spooler, tickerChan <-chan time.Time, blackhole bool) error {
	s.spool = spool
	s.tickerChan = tickerChan
	s.blackhole = blackhole
	go s.run()
	s.logger.Info("Started")
	return nil
}

func (s *Sender) Stop() error {
	s.sync.Stop()
	s.sync.Wait()
	s.spool = nil
	s.tickerChan = nil
	s.logger.Info("Stopped")
	return nil
}

func (s *Sender) Status() map[string]string {
	return s.status.Merge(s.client.Status())
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (s *Sender) run() {
	defer func() {
		if s.sync.IsGraceful() {
			s.logger.Info("Stop")
			s.status.Update("data-sender", "Stopped")
		} else {
			s.logger.Error("Crash")
			s.status.Update("data-sender", "Crashed")
		}
		s.sync.Done()
	}()

	s.logger.Info("Start")
	for {
		s.status.Update("data-sender", "Idle")
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
	s.logger.Debug("send:call")
	defer s.logger.Debug("send:return")

	// Try a few times to connect to the API.
	s.status.Update("data-sender", "Connecting")
	connected := false
	var apiErr error
	for i := 1; i <= 3; i++ {
		if apiErr = s.client.ConnectOnce(); apiErr != nil {
			s.logger.Warn("Connect API failed:", apiErr)
			t := int(5*rand.Float64() + 1) // [1, 5] seconds
			time.Sleep(time.Duration(t) * time.Second)
		} else {
			connected = true
			// client.WebsocketClient expects caller to recv on ConenctChan(),
			// even though in this case we're not using the async channels.
			// todo: fix this poor design assumption/coupling in ws/client.go
			defer func() {
				s.status.Update("data-sender", "Disconnecting")
				s.client.Disconnect()
				<-s.client.ConnectChan()
			}()
			break
		}
	}
	if !connected {
		return
	}
	s.logger.Debug("send:connected")

	maxWarnErr := 3
	n400Err := 0
	n500Err := 0

	// Send all files.
	s.logger.Info("Start sending")
	defer s.logger.Info("Done sending") // todo: log basic numbers: number of files sent, errors, time, etc.

	s.status.Update("data-sender", "Running")
	filesChan := s.spool.Files()
	for file := range filesChan {
		s.logger.Debug("send:" + file)

		s.status.Update("data-sender", "Reading "+file)
		data, err := s.spool.Read(file)
		if err != nil {
			s.logger.Error(err)
			continue
		}

		if s.blackhole {
			s.status.Update("data-sender", "Removing "+file+" (blackhole)")
			s.spool.Remove(file)
			s.logger.Info("Removed " + file + " (blackhole)")
			continue
		}

		// todo: number/time/rate limit so we dont DDoS API
		s.status.Update("data-sender", "Sending "+file)
		if err := s.client.SendBytes(data); err != nil {
			s.logger.Warn(err)
			continue
		}

		s.status.Update("data-sender", "Waiting for API to ack "+file)
		resp := &proto.Response{}
		if err := s.client.Recv(resp, 5); err != nil {
			s.logger.Warn(err)
			continue
		}
		s.logger.Debug(fmt.Sprintf("send:resp:%+v", resp.Code))
		if resp.Code != 200 && resp.Code != 201 {
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
				s.logger.Warn(resp)
			}
			continue
		}

		s.status.Update("data-sender", "Removing "+file)
		s.spool.Remove(file)
		s.logger.Info("Sent and removed", file)
	}

	if n400Err > maxWarnErr {
		s.logger.Warn(fmt.Sprintf("%d more 4xx errors", n400Err-maxWarnErr))
	}
	if n500Err > maxWarnErr {
		s.logger.Warn(fmt.Sprintf("%d more 5xx errors", n500Err-maxWarnErr))
	}
}
