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
			s.logger.Info("sendData stop")
		} else {
			s.logger.Error("sendData crash")
		}
		s.sync.Done()
	}()

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
	for i := 1; i <= 3; i++ {
		if err := s.client.ConnectOnce(); err != nil {
			s.logger.Warn("Connect API failed:", err)
			t := int(5*rand.Float64() + 1) // [1, 5] seconds
			time.Sleep(time.Duration(t) * time.Second)
		} else {
			connected = true
			defer s.client.Disconnect()
			break
		}
	}
	if !connected {
		return
	}

	maxWarnErr := 3
	n400Err := 0
	n500Err := 0

	// Send all files.
	// todo: number/time/rate limit so we dont DDoS API
	s.logger.Debug("Start sending")
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

	s.logger.Debug("Done sending")
}
