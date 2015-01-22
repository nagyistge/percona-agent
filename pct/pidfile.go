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
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type PidFile struct {
	name string
	mux  *sync.RWMutex
}

func NewPidFile() *PidFile {
	p := &PidFile{
		mux: new(sync.RWMutex),
	}
	return p
}

func (p *PidFile) Get() string {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.name
}

func (p *PidFile) Set(pidFile string) error {
	/**
	 * Get new PID file _then_ remove old, i.e. don't give up current PID file
	 * until new PID file is secured.  If no new PID file, then just remove old
	 * (if any) and return early.
	 */
	p.mux.Lock()
	defer p.mux.Unlock()

	// Return early if no new PID file.
	if pidFile == "" {
		// Remove existing PID file if any.  Do NOT call Remove() because it locks.
		if err := p.remove(); err != nil {
			return err
		}
		return nil
	}

	// The following needs clarification
	// Relative paths could contain '../' in any number of places, some may yield a valid
	// path inside basedir, others not. Absolute paths can only be included in basedir.
	// The approach is to transform everything to absolute paths, and check if it lands
	// inside basedir by checking its path relativeness against basedir.
	if !filepath.IsAbs(pidFile) {
		// Path is relative to basedir, make an absolute path
		pidFile = filepath.Join(Basedir.Path(), pidFile)
	}
	// Check that path lands inside basedir by calculating a relative path to basedir and
	// looking for '../'s
	if relPath, err := filepath.Rel(Basedir.Path(), pidFile); err == nil {
		if contains := strings.Contains(relPath, "../"); contains == true {
			return errors.New("pidfile path should be relative to basedir")
		}
	} else {
		return errors.New("Could not determine if pidfile is relative to basedir, please check your pidfile path")
	}

	// Create new PID file, success only if it doesn't already exist.
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(pidFile, flags, 0644)
	if err != nil {
		return err
	}

	// Write PID to new PID file and close.
	if _, err := file.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	// Remove old PID file if any.  Do NOT call Remove() because it locks.
	if err := p.remove(); err != nil {
		// If this happens we're in a weird state: holding both PID files.
		return err
	}

	// Success: new PID file set, old removed.
	p.name = pidFile
	return nil
}

func (p *PidFile) Remove() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	if p.name == "" {
		return nil
	}
	return p.remove()
}

func (p *PidFile) remove() error {
	// Do NOT lock here.  Expect caller to lock.
	if err := os.Remove(p.name); err != nil && !os.IsNotExist(err) {
		return err
	}
	p.name = ""
	return nil
}
