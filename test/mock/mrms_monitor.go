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

package mock

import (
	"time"
)

type MrmsMonitor struct {
	c chan bool
}

func NewMrmsMonitor() *MrmsMonitor {
	m := &MrmsMonitor{}
	return m
}

func (m *MrmsMonitor) Add(dsn string) (<-chan bool, error) {
	m.c = make(chan bool, 10)
	return m.c, nil
}

func (m *MrmsMonitor) Remove(dsn string, c <-chan bool) {
}

func (m *MrmsMonitor) Check() {
}

func (m *MrmsMonitor) Start(interval time.Duration) error {
	return nil
}

func (m *MrmsMonitor) Stop() error {
	return nil
}

func (m *MrmsMonitor) Status() (status map[string]string) {
	return map[string]string{
		"mrms-monitor-mock": "Idle",
	}
}

// The restartChan in the real MrmsMonitor is read only.
// To be consistent with that, instead of returning the channel just for
// testing purposes, we have this method to simulate a MySQL restart
func (m *MrmsMonitor) SimulateMySQLRestart() {
	m.c <- true
}
