package data

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"time"
)

type Manager struct {
	logger   *pct.Logger
	hostname string
	client   pct.WebsocketClient
	// --
	config  *Config
	sz      Serializer
	spooler Spooler
	sender  *Sender
	status  *pct.Status
}

func NewManager(logger *pct.Logger, hostname string, client pct.WebsocketClient) *Manager {
	m := &Manager{
		logger:   logger,
		hostname: hostname,
		client:   client,
		// --
		status: pct.NewStatus([]string{"data"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	if m.config != nil {
		err := pct.ServiceIsRunningError{Service: "data"}
		return err
	}

	// proto.Cmd[Service:agent, Cmd:StartService, Data:proto.ServiceData[Name:data Config:data.Config]]
	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	var sz Serializer
	switch c.Encoding {
	case "":
		sz = NewJsonSerializer()
	case "gzip":
		sz = NewJsonGzipSerializer()
	default:
		return errors.New("Unknown encoding: " + c.Encoding)
	}

	spooler := NewDiskvSpooler(
		pct.NewLogger(m.logger.LogChan(), "data-spooler"),
		c.Dir,
		sz,
		m.hostname,
	)
	if err := spooler.Start(); err != nil {
		return err
	}
	m.spooler = spooler
	m.logger.Info("Started spooler")

	sender := NewSender(
		pct.NewLogger(m.logger.LogChan(), "data-sender"),
		m.client,
		m.spooler,
		time.Tick(time.Duration(c.SendInterval)*time.Second),
	)
	if err := sender.Start(); err != nil {
		return err
	}
	m.sender = sender
	m.logger.Info("Started sender")

	m.config = c

	m.status.Update("data", "Ready")
	m.logger.Info("Ready")
	return nil
}

// @goroutine[0]
func (m *Manager) Stop(cmd *proto.Cmd) error {
	// Can't stop data yet.
	return nil
}

// @goroutine[0]
func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	defer m.status.Update("data", "Ready")
	switch cmd.Cmd {
	case "GetConfig":
		// proto.Cmd[Service:data, Cmd:GetConfig]
		return cmd.Reply(m.config)
	case "Status":
		// proto.Cmd[Service:data, Cmd:Status]
		status := m.internalStatus()
		return cmd.Reply(status)
	default:
		// todo: dynamic config
		return cmd.Reply(pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

// @goroutine[0:1]
func (m *Manager) Status() string {
	return m.status.Get("data", true)
}

// @goroutine[0]
func (m *Manager) internalStatus() map[string]string {
	s := make(map[string]string)
	s["data"] = m.Status()
	return s
}

func (m *Manager) Spooler() Spooler {
	return m.spooler
}

func (m *Manager) Sender() *Sender {
	return m.sender
}
