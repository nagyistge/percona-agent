package mm

import (
	"encoding/json"
	proto "github.com/percona/cloud-protocol"
	pct "github.com/percona/cloud-tools"
)

type Manager struct {
	logger   *pct.Logger
	dataChan chan interface{}
	// --
	config   *Config // nil if not running
	status   *pct.Status
	monitors map[string]Monitor
}

func NewManager(logger *pct.Logger, dataChan chan interface{}) *Manager {
	monitors := map[string]Monitor{
		"collectd":  nil,
		"mysql":     nil,
		"pctagentd": nil,
		"system":    nil,
	}
	m := &Manager{
		logger:   logger,
		dataChan: dataChan,
		// --
		config:   nil, // not running yet
		monitors: monitors,
		status:   pct.NewStatus([]string{"mm"}),
	}
	return m
}

func (m *Manager) Start(cmd *proto.Cmd, config []byte) error {
	c := new(Config)
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}

	m.config = c
	return nil
}

func (m *Manager) Stop(cmd *proto.Cmd) error {
	for _, monitor := range m.monitors {
		monitor.Stop()
	}
	return nil
}

func (m *Manager) Status() string {
	return ""
}

func (m *Manager) IsRunning() bool {
	return false
}

func (m *Manager) Do(cmd *proto.Cmd) error {
	mmData := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, mmData); err != nil {
		return err
	}

	monitor, haveMonitor := m.monitors[mmData.Name]
	if !haveMonitor {
		// err
	}

	var err error
	switch cmd.Cmd {
	case "Start":
		err = monitor.Start(mmData.Config)
	case "Stop":
		err = monitor.Stop()
	default:
		err = pct.UnknownCmdError{Cmd: cmd.Cmd}
	}

	return err
}
