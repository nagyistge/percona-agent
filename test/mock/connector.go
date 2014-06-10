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
	"database/sql"
	"github.com/percona/percona-agent/mysql"
)

type ConnectorMock struct {
	DBMock                 func() *sql.DB
	DSNMock                func() string
	ConnectMock            func(tries uint) error
	CloseMock              func()
	SetMock                func([]mysql.Query) error
	GetGlobalVarStringMock func(varName string) string
	UptimeMock             func() (uptime uint)
}

func NewConnectorMock() *ConnectorMock {
	return &ConnectorMock{}
}

func (c *ConnectorMock) DB() *sql.DB {
	return c.DBMock()
}

func (c *ConnectorMock) DSN() string {
	return c.DSNMock()
}

func (c *ConnectorMock) Connect(tries uint) error {
	return c.ConnectMock(tries)
}

func (c *ConnectorMock) Close() {
	c.CloseMock()
}

func (c *ConnectorMock) Set(queries []mysql.Query) error {
	return c.SetMock(queries)
}

func (c *ConnectorMock) GetGlobalVarString(varName string) string {
	return c.GetGlobalVarStringMock(varName)
}

func (c *ConnectorMock) Uptime() uint {
	return c.UptimeMock()
}
