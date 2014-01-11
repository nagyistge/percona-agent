package pct

import (
	proto "github.com/percona/cloud-protocol"
)

type ServiceManager interface {
	// @goroutine[0]
	Start(cmd *proto.Cmd, config []byte) error
	Stop(cmd *proto.Cmd) error
	IsRunning() bool
	Handle(cmd *proto.Cmd) error
	// @goroutine[1]
	Status() string
}
