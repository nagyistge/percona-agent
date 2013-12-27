package pct

import (
	proto "github.com/percona/cloud-protocol"
)

type ServiceManager interface {
	Start(cmd *proto.Cmd, data []byte) error
	Stop(cmd *proto.Cmd) error
	Status() string
	IsRunning() bool
	Do(cmd *proto.Cmd) error
}
