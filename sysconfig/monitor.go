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

package sysconfig

import (
	"time"
)

type Monitor interface {
	Start(config []byte, tickChan chan time.Time, sysconfigChan chan *SystemConfig) error
	Stop() error
	Status() map[string]string
	TickChan() chan time.Time
	Config() interface{}
}

type MonitorFactory interface {
	Make(mtype, name string) (Monitor, error)
}

// ["variable", "value"]
type Setting [2]string

type SystemConfig struct {
	Ts     int64 // UTC Unix timestamp
	System string
	Config []Setting
}
