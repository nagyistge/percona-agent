package data

import (
	"github.com/percona/cloud-tools/pct"
)

type Sender struct {
	logger     *pct.Logger
	client     pct.HttpClient
	url        string
	spool      Spooler
	dataDir    string
	tickerChan chan bool
	// --
	sync *pct.SyncChan
}

func NewSender(logger *pct.Logger, client pct.HttpClient, url string, spool Spooler, tickerChan chan bool) *Sender {
	s := &Sender{
		logger:     logger,
		client:     client,
		url:        url,
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
			s.logger.Debug("Start sending")
			filesChan := s.spool.Files()
			for file := range filesChan {
				data, err := s.spool.Read(file)
				if err != nil {
					s.logger.Error(err)
					continue
				}

				// POST the data
				s.logger.Debug("Sending", file)
				err = s.client.Post(s.url, data)
				if err != nil {
					s.logger.Error(err)
				} else {
					s.logger.Info("Sent", file)
					s.spool.Remove(file)
				}
			}
			s.logger.Debug("Done sending")
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}
