package pct

import (
	"fmt"
)

type ServiceIsRunningError struct {
	Service string
}

func (e ServiceIsRunningError) Error() string {
	return e.Service + "service is running"
}

/////////////////////////////////////////////////////////////////////////////

type ServiceIsNotRunningError struct {
	Service string
}

func (e ServiceIsNotRunningError) Error() string {
	return e.Service + "service is not running"
}

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
	Cmd  string
	Name string
	Size uint
}

func (e QueueFullError) Error() string {
	err := fmt.Sprintf("Cannot handle %s command because the %s queue is full (size: %d messages)\n",
		e.Cmd, e.Name, e.Size)
	return err
}

/////////////////////////////////////////////////////////////////////////////

type CmdRejectedError struct {
	Cmd    string
	Reason string
}

func (e CmdRejectedError) Error() string {
	return fmt.Sprintf("%s command rejected because %s", e.Cmd, e.Reason)
}
