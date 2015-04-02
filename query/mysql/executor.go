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

package mysql

import (
	"database/sql"
	"fmt"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mysql"
)

type QueryExecutor struct {
	conn mysql.Connector
}

func NewQueryExecutor(conn mysql.Connector) *QueryExecutor {
	e := &QueryExecutor{
		conn: conn,
	}
	return e
}

func (e *QueryExecutor) Explain(db, query string) (*proto.ExplainResult, error) {
	explain, err := e.explain(db, query)
	if err != nil {
		if mysql.MySQLErrorCode(err) == mysql.ER_SYNTAX_ERROR {
			if IsDMLQuery(query) {
				query = DMLToSelect(query)
				if query == "" {
					return nil, fmt.Errorf("Cannot convert non-SELECT query")
				}
				explain, err = e.explain(db, query)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("EXPLAIN failed: %s", err)
		}
	}
	return explain, nil
}

func (e *QueryExecutor) ShowCreateTable(db, table string) (string, error) {
	return "", nil
}

func (e *QueryExecutor) ShowIndexes(db, table string) (string, error) {
	return "", nil
}

func (e *QueryExecutor) ShowTableStatus(db, table string) (string, error) {
	return "", nil
}

// --------------------------------------------------------------------------

func (e *QueryExecutor) explain(db, query string) (*proto.ExplainResult, error) {
	// Transaction because we need to ensure USE and EXPLAIN are run in one connection
	tx, err := e.conn.DB().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// If the query has a default db, use it; else, all tables need to be db-qualified
	// or EXPLAIN will throw an error.
	if db != "" {
		_, err := tx.Exec(fmt.Sprintf("USE %s", db))
		if err != nil {
			return nil, err
		}
	}

	classicExplain, err := e.classicExplain(tx, query)
	if err != nil {
		return nil, err
	}

	jsonExplain, err := e.jsonExplain(tx, query)
	if err != nil {
		return nil, err
	}

	explain := &proto.ExplainResult{
		Classic: classicExplain,
		JSON:    jsonExplain,
	}

	return explain, nil
}

func (e *QueryExecutor) classicExplain(tx *sql.Tx, query string) (classicExplain []*proto.ExplainRow, err error) {
	// Partitions are introduced since MySQL 5.1
	// We can simply run EXPLAIN /*!50100 PARTITIONS*/ to get this column when it's available
	// without prior check for MySQL version.
	rows, err := tx.Query(fmt.Sprintf("EXPLAIN /*!50100 PARTITIONS*/ %s", query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Go rows.Scan() expects exact number of columns
	// so when number of columns is undefined then the easiest way to
	// overcome this problem is to count received number of columns
	// With 'partitions' it is 11 columns
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	hasPartitions := len(columns) == 11

	for rows.Next() {
		explainRow := &proto.ExplainRow{}
		if hasPartitions {
			err = rows.Scan(
				&explainRow.Id,
				&explainRow.SelectType,
				&explainRow.Table,
				&explainRow.Partitions, // Since MySQL 5.1
				&explainRow.Type,
				&explainRow.PossibleKeys,
				&explainRow.Key,
				&explainRow.KeyLen,
				&explainRow.Ref,
				&explainRow.Rows,
				&explainRow.Extra,
			)
		} else {
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
		}
		if err != nil {
			return nil, err
		}
		classicExplain = append(classicExplain, explainRow)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return classicExplain, nil
}

func (e *QueryExecutor) jsonExplain(tx *sql.Tx, query string) (string, error) {
	// EXPLAIN in JSON format is introduced since MySQL 5.6.5
	ok, err := e.conn.AtLeastVersion("5.6.5")
	if !ok || err != nil {
		return "", err
	}

	explain := ""
	err = tx.QueryRow(fmt.Sprintf("EXPLAIN FORMAT=JSON %s", query)).Scan(&explain)
	if err != nil {
		return "", err
	}

	return explain, nil
}
