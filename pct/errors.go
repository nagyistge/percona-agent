/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package pct

import (
	"errors"
	"fmt"
)

type ServiceIsRunningError struct {
	Service string
}

func (e ServiceIsRunningError) Error() string {
	return e.Service + " service is running"
}

/////////////////////////////////////////////////////////////////////////////

type ServiceIsNotRunningError struct {
	Service string
}

func (e ServiceIsNotRunningError) Error() string {
	return e.Service + " service is not running"
}

/////////////////////////////////////////////////////////////////////////////

type UnknownServiceError struct {
	Service string
}

func (e UnknownServiceError) Error() string {
	return "Unknown service: " + e.Service
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

/////////////////////////////////////////////////////////////////////////////

type UnknownToolInstanceError struct {
	Tool string
	UUID string
}

func (e UnknownToolInstanceError) Error() string {
	return fmt.Sprintf("Unknown %s instance: %d", e.Tool, e.UUID)
}

/////////////////////////////////////////////////////////////////////////////

type InvalidToolInstanceError struct {
	Tool string
	UUID string
}

func (e InvalidToolInstanceError) Error() string {
	return fmt.Sprintf("Invalid %s instance: %d", e.Tool, e.UUID)
}

/////////////////////////////////////////////////////////////////////////////

type DuplicateToolInstanceError struct {
	Tool string
	UUID string
}

func (e DuplicateToolInstanceError) Error() string {
	return fmt.Sprintf("Duplicate %s instance: %d", e.Tool, e.UUID)
}

////////////////////////////////////////////////////////////////////////////

type InvalidInstanceError struct {
	UUID string
}

func (e InvalidInstanceError) Error() string {
	return fmt.Sprintf("Invalid instance: %s", e.UUID)
}

/////////////////////////////////////////////////////////////////////////////

type ToolIsNotRunningError struct {
	Tool     string
	InstName string
}

func (e ToolIsNotRunningError) Error() string {
	return fmt.Sprintf("%s tool for instance %s is not running", e.Tool, e.InstName)
}

// Error variables
////////////////////////////////////////////////////////////////////////////
var ErrNoSystemTree error = errors.New("No local system tree file")
