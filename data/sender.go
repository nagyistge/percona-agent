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
	"time"
)

const (
	MAX_SEND_ERRORS    = 3
	CONNECT_ERROR_WAIT = 3
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
	// --
	sent   uint
	errs   uint
	bad    uint
	apiErr bool
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

	s.sent = 0
	s.errs = 0
	s.bad = 0
	s.apiErr = false
	defer func() {
		s.logger.Debug(fmt.Sprintf("sent:%d bad:%d errs:%d apiErr:%t", s.sent, s.bad, s.errs, s.apiErr))

		s.status.Update("data-sender", "Disconnecting")
		s.client.DisconnectOnce()
		if s.sent == 0 && !s.apiErr {
			s.logger.Warn("No data sent")
		}
		if s.errs > 0 || s.bad > 0 || s.apiErr {
			s.status.Update("data-sender", fmt.Sprintf("Idle (last sent at %s: %d ok, %d bad, %d error, API error %t)",
				time.Now(), s.sent, s.bad, s.errs, s.apiErr))
		} else {
			s.status.Update("data-sender", fmt.Sprintf("Idle (last sent at %s: all %d files ok)", time.Now(), s.sent))
		}
	}()

	// Connect and send files until too many errors occur.
	for !s.apiErr && s.errs < MAX_SEND_ERRORS {
		s.status.Update("data-sender", "Connecting")
		s.logger.Debug("send:connecting")
		if s.errs > 0 {
			time.Sleep(CONNECT_ERROR_WAIT * time.Second)
		}
		if err := s.client.ConnectOnce(10); err != nil {
			s.logger.Warn("Cannot connect to API: ", err)
			s.errs++
			continue
		}
		s.logger.Debug("send:connected")

		if err := s.sendAllFiles(); err != nil {
			s.logger.Debug("sendAllFiles:err:", err)
			s.errs++
			s.logger.Warn(err)
			s.client.DisconnectOnce()
			continue // error sending files, re-connect and try again
		}
		return // success or API error, either way, stop sending
	}
}

func (s *Sender) sendAllFiles() error {
	s.status.Update("data-sender", "Running")
	for file := range s.spool.Files() {
		s.logger.Debug("send:" + file)

		s.status.Update("data-sender", "Reading "+file)
		data, err := s.spool.Read(file)
		if err != nil {
			return fmt.Errorf("spool.Read: %s", err)
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
			return fmt.Errorf("Sending %s: %s", file, err)
		}

		s.status.Update("data-sender", "Waiting for API to ack "+file)
		resp := &proto.Response{}
		if err := s.client.Recv(resp, 5); err != nil {
			return fmt.Errorf("Waiting for API to ack %s: %s", file, err)
		}
		s.logger.Debug(fmt.Sprintf("send:resp:%+v", resp.Code))

		switch {
		case resp.Code >= 500:
			// API had problem, try sending files again later.
			s.apiErr = true
			return nil // don't warn about API errors
		case resp.Code >= 400:
			// File is bad, remove it.
			s.status.Update("data-sender", "Removing "+file)
			s.spool.Remove(file)
			s.logger.Warn(fmt.Sprintf("Removed %s because API returned %d", file, resp.Code))
			s.sent++
			s.bad++
		case resp.Code >= 300:
			// This shouldn't happen.
			return fmt.Errorf("Recieved unhandled response code from API: %d", resp.Code)
		case resp.Code >= 200:
			s.status.Update("data-sender", "Removing "+file)
			s.spool.Remove(file)
			s.sent++
		default:
			// This shouldn't happen.
			return fmt.Errorf("Recieved unknown response code from API: %d", resp.Code)
		}
	}
	return nil // success
}
