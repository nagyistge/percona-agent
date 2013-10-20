package agent

import (
	"fmt"
)

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

/////////////////////////////////////////////////////////////////////////////

type UnknownServiceError struct {
	Service string
}

func (e UnknownServiceError) Error() string {
	return "Agent does not have a service manager for" + e.Service
}

/////////////////////////////////////////////////////////////////////////////

type CmdTimeoutError struct {
	Cmd string
}

func (e CmdTimeoutError) Error() string {
	return "Timeout waiting for " + e.Cmd
}

/////////////////////////////////////////////////////////////////////////////

type UnknownCmdError struct {
	Cmd string
}

func (e UnknownCmdError) Error() string {
	return "Unknown command: " + e.Cmd
}

/////////////////////////////////////////////////////////////////////////////

type QueueFullError struct {
	Cmd string
	Name string
	Size uint
	Queue []*proto.Msg
}

func (e QueueFullError) Error() string {
	err := fmt.Sprintf("Cannot handle %s command because the %s queue is full (size: %d messages):\n",
		e.Cmd, e.Name, e.Size)
	for i, msg := range e.Queue {
		err += fmt.Sprintf("%d. %s\n", i, msg)
	}
	return err
}

/////////////////////////////////////////////////////////////////////////////

type CmdRejectedError struct {
	Cmd string
	Reason string
}

func (e CmdRejectedError) Error() string {
	return fmt.Sprintf("%s command rejected because %s", e.Cmd, e.Reason)
}

