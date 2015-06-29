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

package mock

import (
	"database/sql"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/cloud-protocol/proto/v2/qan"
)

type NullMySQL struct {
	set               []qan.ConfigQuery
	explain           map[string]*proto.ExplainResult
	uptime            int64
	uptimeCount       uint
	stringVars        map[string]string
	numberVars        map[string]float64
	SetChan           chan bool
	atLeastVersion    bool
	atLeastVersionErr error
	Version           string
}

func NewNullMySQL() *NullMySQL {
	n := &NullMySQL{
		set:        []qan.ConfigQuery{},
		explain:    make(map[string]*proto.ExplainResult),
		stringVars: make(map[string]string),
		numberVars: make(map[string]float64),
		SetChan:    make(chan bool),
	}
	return n
}

func (n *NullMySQL) DB() *sql.DB {
	return nil
}

func (n *NullMySQL) DSN() string {
	return "user:pass@tcp(127.0.0.1:3306)/?parseTime=true"
}

func (n *NullMySQL) Connect(tries uint) error {
	return nil
}

func (n *NullMySQL) Close() {
	return
}

func (n *NullMySQL) Explain(query string, db string) (explain *proto.ExplainResult, err error) {
	return n.explain[query], nil
}

func (n *NullMySQL) SetExplain(query string, explain *proto.ExplainResult) {
	n.explain[query] = explain
}

func (n *NullMySQL) Set(queries []qan.ConfigQuery) error {
	for _, q := range queries {
		n.set = append(n.set, q)
	}
	select {
	case n.SetChan <- true:
	default:
	}
	return nil
}

func (n *NullMySQL) GetSet() []qan.ConfigQuery {
	return n.set
}

func (n *NullMySQL) Reset() {
	n.set = nil
	n.stringVars = make(map[string]string)
	n.numberVars = make(map[string]float64)
}

func (n *NullMySQL) GetGlobalVarString(varName string) string {
	value, ok := n.stringVars[varName]
	if ok {
		return value
	}
	return ""
}

func (n *NullMySQL) GetGlobalVarNumber(varName string) float64 {
	value, ok := n.numberVars[varName]
	if ok {
		return value
	}
	return 0
}

func (n *NullMySQL) SetGlobalVarNumber(name string, value float64) {
	n.numberVars[name] = value
}

func (n *NullMySQL) SetGlobalVarString(name, value string) {
	n.stringVars[name] = value
}

func (n *NullMySQL) Uptime() (int64, error) {
	n.uptimeCount++
	return n.uptime, nil
}

func (n *NullMySQL) AtLeastVersion(v string) (bool, error) {
	n.Version = v
	return n.atLeastVersion, n.atLeastVersionErr
}

func (n *NullMySQL) SetAtLeastVersion(atLeastVersion bool, err error) {
	n.atLeastVersion = atLeastVersion
	n.atLeastVersionErr = err
}

func (n *NullMySQL) GetUptimeCount() uint {
	return n.uptimeCount
}

func (n *NullMySQL) SetUptime(uptime int64) {
	n.uptime = uptime
}
