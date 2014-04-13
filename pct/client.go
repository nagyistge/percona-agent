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
	"code.google.com/p/go.net/websocket"
	"github.com/percona/cloud-protocol/proto"
)

type WebsocketClient interface {
	Connect()
	ConnectOnce() error
	Disconnect() error
	Status() map[string]string

	// Channel interface:
	Start()
	Stop()
	SendChan() chan *proto.Reply
	RecvChan() chan *proto.Cmd
	ConnectChan() chan bool
	ErrorChan() chan error

	// Direct interface:
	SendBytes(data []byte) error
	Send(data interface{}, timeout uint) error
	Recv(data interface{}, timeout uint) error
	Conn() *websocket.Conn
}
