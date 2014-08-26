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
	//	"math/rand"
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
		if err := recover(); err != nil {
			s.logger.Error("Data sender crashed: ", err)
		}
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
	s.status.Update("data-sender", "Idle")
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
	s.logger.Debug("send:call")
	defer s.logger.Debug("send:return")

	sentOK := 0
	errCount := 0
	defer func() {
		if sentOK == 0 {
			s.logger.Warn(fmt.Sprintf("No data sent"))
		}
		s.status.Update("data-sender", fmt.Sprintf("Idle (last sent %d files at %s, %d errors)", sentOK, time.Now(), errCount))
		s.status.Update("data-sender", "Disconnecting")
		s.client.DisconnectOnce()
	}()

	for errCount < 3 {
		s.status.Update("data-sender", "Connecting")
		s.logger.Debug("send:connecting")
		if err := s.client.ConnectOnce(10); err != nil {
			s.logger.Warn("Cannot connect to API: ", err)
			time.Sleep(3 * time.Second)
			continue
		}
		s.logger.Debug("send:connected")

		sent, err := s.sendAllFiles()
		sentOK += sent
		if err == nil {
			return // success
		}

		// Error, try again maybe.
		errCount++
		s.logger.Warn(err)
		s.client.DisconnectOnce()
	}
}

func (s *Sender) sendAllFiles() (int, error) {
	s.status.Update("data-sender", "Running")
	sent := 0
	for file := range s.spool.Files() {
		s.logger.Debug("send:" + file)

		s.status.Update("data-sender", "Reading "+file)
		data, err := s.spool.Read(file)
		if err != nil {
			return sent, fmt.Errorf("spool.Read: %s", err)
		}

		if s.blackhole {
			s.status.Update("data-sender", "Removing "+file+" (blackhole)")
			s.spool.Remove(file)
			s.logger.Info("Removed " + file + " (blackhole)")
			continue // next file
		}

		if len(data) == 0 {
			s.spool.Remove(file)
			s.logger.Warn("Removed " + file + " because it's empty")
			continue // next file
		}

		// todo: number/time/rate limit so we dont DDoS API
		s.status.Update("data-sender", "Sending "+file)
		if err := s.client.SendBytes(data); err != nil {
			return sent, fmt.Errorf("Sending %s: %s", file, err)
		}

		s.status.Update("data-sender", "Waiting for API to ack "+file)
		resp := &proto.Response{}
		if err := s.client.Recv(resp, 5); err != nil {
			return sent, fmt.Errorf("Waiting for API to ack %s: %s", file, err)
		}
		s.logger.Debug(fmt.Sprintf("send:resp:%+v", resp.Code))

		if resp.Code != 200 && resp.Code != 201 {
			return sent, fmt.Errorf("Error sending %s: %s", file, resp)
		}

		s.status.Update("data-sender", "Removing "+file)
		s.spool.Remove(file)
		sent++
	}
	return sent, nil
}
