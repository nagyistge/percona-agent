package service

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type Manager interface {
	Start(msg *proto.Msg, config []byte) error
	Stop(msg *proto.Msg) error
	Status() string
	IsRunning() bool
}
