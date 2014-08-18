/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

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

package cmd

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

var (
	DefaultTimeout = 30 * time.Second
)

var (
	ErrNotFound                = errors.New("Executable file not found in $PATH")
	ErrTimeout                 = errors.New("Timeout")
	ErrKillProcessAfterTimeout = errors.New("Failed to kill process after timeout")
)

type Cmd struct {
	Timeout time.Duration
	name    string
	arg     []string
}

type result struct {
	output string
	err    error
}

func New(name string, arg ...string) *Cmd {
	return &Cmd{
		name:    name,
		arg:     arg,
		Timeout: DefaultTimeout,
	}
}

func (c *Cmd) Run() (output string, err error) {
	cmd := exec.Command(c.name, c.arg...)

	// Workaround for "HOME: parameter not set"
	if os.Getenv("HOME") == "" {
		cmd.Env = append(os.Environ(), "HOME=/root")
	}

	resultChan := runCmd(cmd)
	select {
	case <-time.After(c.Timeout):
		killErr := cmd.Process.Kill()
		if killErr != nil {
			// @todo:
			// If this happens that means leaving working process,
			// plus working goroutine waiting for that process to finish.
			// And since this command is going to be run over, and over again
			// we might end up with hundreds processes and goroutines hanging.
			// Maybe in such critical cases (or after n-cases) we should shutdown whole module (e.g. qan/mm/summary)
			// and notify us (developers), because this shouldn't happen in correct working program - but you never know
			return "", ErrKillProcessAfterTimeout
		}
		return "", ErrTimeout
	case result := <-resultChan:
		execError, ok := result.err.(*exec.Error)
		if ok && execError.Err == exec.ErrNotFound {
			return "", ErrNotFound
		}
		return result.output, result.err
	}
}

func runCmd(cmd *exec.Cmd) (resultChan chan result) {
	// Below channels has buffer
	// because we might get data before we would be waiting on this channel
	resultChan = make(chan result, 1)
	go func() {
		output, err := cmd.CombinedOutput()
		select {
		case resultChan <- result{output: string(output), err: err}:
		default:
		}
	}()
	return resultChan
}
