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

package pct

import (
	"github.com/percona/cloud-protocol/proto"
)

type ServiceManager interface {
	// @goroutine[0]
	Start(cmd *proto.Cmd, config []byte) error
	Stop(cmd *proto.Cmd) error
	Handle(cmd *proto.Cmd) *proto.Reply
	LoadConfig(configDir string) (interface{}, error)
	WriteConfig(config interface{}, name string) error
	RemoveConfig(name string) error
	// @goroutine[1]
	Status() map[string]string
}
