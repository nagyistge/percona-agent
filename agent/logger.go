package agent

// Implemented by agent/ws/logger.go and test/mock/logger.go

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type Logger interface {
	NewLogger(client *proto.Client)
	SetLogLevel(string)
	Debug(string)
	Info(string)
	Warn(string)
	Err(string)
	Fatal(string)
}
