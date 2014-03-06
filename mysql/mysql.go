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

package mysql

import (
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
)

type Connector interface {
	Connect(dsn string) error
	Set([]Query) error
	GetGlobalVarString(varName string) string
}

type Connection struct {
	dsn  string
	conn *sql.DB
}

func (c *Connection) DB() *sql.DB {
	return c.conn
}

func (c *Connection) Connect(dsn string) error {
	if c.conn != nil {
		c.conn.Close()
	}

	// Open connection to MySQL but...
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	// ...try to use the connection for real.
	if err := db.Ping(); err != nil {
		// Connection failed.  Wrong username or password?
		db.Close()
		return err
	}

	c.dsn = dsn
	c.conn = db

	// Connected
	return nil
}

func (c *Connection) Set(queries []Query) error {
	if c.conn == nil {
		return errors.New("Not connected")
	}
	for _, query := range queries {
		if _, err := c.conn.Exec(query.Set); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) GetGlobalVarString(varName string) string {
	if c.conn == nil {
		return ""
	}
	var varValue string
	c.conn.QueryRow("SELECT @@GLOBAL." + varName).Scan(&varValue)
	return varValue
}

func (c *Connection) GetGlobalVarNumber(varName string) float64 {
	if c.conn == nil {
		return 0
	}
	var varValue float64
	c.conn.QueryRow("SELECT @@GLOBAL." + varName).Scan(&varValue)
	return varValue
}
