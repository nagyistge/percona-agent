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
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
	"time"
)

type Connector interface {
	DB() *sql.DB
	DSN() string
	Connect(tries uint) error
	Close()
	Explain(q string) (explain *Explain, err error)
	Set([]Query) error
	GetGlobalVarString(varName string) string
}

type Connection struct {
	dsn     string
	conn    *sql.DB
	backoff *pct.Backoff
}

// @todo move ExplainRow and NullString to proto
// @todo not all rows are included
type ExplainRow struct {
	Id           NullInt64
	SelectType   NullString
	Table        NullString
	CreateTable  NullString
	Type         NullString
	PossibleKeys NullString // maybe map
	Key          NullString
	KeyLen       NullInt64
	Ref          NullString
	Rows         NullInt64
	Extra        NullString
}

type NullString struct {
	sql.NullString
}

func (n NullString) MarshalJSON() (b []byte, err error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.String)
}

func (n *NullString) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		n.String = ""
		n.Valid = false
		return nil
	}
	err := json.Unmarshal(b, &n.String)
	if err != nil {
		return err
	}
	n.Valid = true
	return nil
}

type NullInt64 struct {
	sql.NullInt64
}

func (n NullInt64) MarshalJSON() (b []byte, err error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Int64)
}

func (n *NullInt64) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		n.Int64 = 0
		n.Valid = false
		return nil
	}
	err := json.Unmarshal(b, &n.Int64)
	if err != nil {
		return err
	}
	n.Valid = true
	return nil
}

type ExplainQuery struct {
	proto.ServiceInstance
	Query string
}

type Explain struct {
	Result []ExplainRow
}

// @todo all above to proto

func NewConnection(dsn string) *Connection {
	c := &Connection{
		dsn:     dsn,
		backoff: pct.NewBackoff(20 * time.Second),
	}
	return c
}

func (c *Connection) DB() *sql.DB {
	return c.conn
}

func (c *Connection) DSN() string {
	return c.dsn
}

func (c *Connection) Connect(tries uint) error {
	if tries == 0 {
		return nil
	}

	var err error
	var db *sql.DB
	for i := tries; i > 0; i-- {
		// Wait before attempt.
		time.Sleep(c.backoff.Wait())

		// Open connection to MySQL but...
		db, err = sql.Open("mysql", c.dsn)
		if err != nil {
			continue
		}

		// ...try to use the connection for real.
		if err = db.Ping(); err != nil {
			// Connection failed.  Wrong username or password?
			db.Close()
			continue
		}

		// Connected
		c.conn = db
		c.backoff.Success()
		return nil
	}

	return errors.New(fmt.Sprintf("Failed to connect to MySQL after %d tries (%s)", tries, err))
}

func (c *Connection) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Connection) Explain(q string) (explain *Explain, err error) {
	rows, err := c.conn.Query(fmt.Sprintf("EXPLAIN %s", q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	explain = &Explain{}
	for rows.Next() {
		explainRow := ExplainRow{}
		err = rows.Scan(
			&explainRow.Id,
			&explainRow.SelectType,
			&explainRow.Table,
			&explainRow.Type,
			&explainRow.PossibleKeys,
			&explainRow.Key,
			&explainRow.KeyLen,
			&explainRow.Ref,
			&explainRow.Rows,
			&explainRow.Extra,
		)
		if err != nil {
			return nil, err
		}
		explain.Result = append(explain.Result, explainRow)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return explain, nil // success
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
