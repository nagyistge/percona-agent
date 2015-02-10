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

package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/instance"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
)

const (
	SERVICE_NAME = "explain"
)

type Explain struct {
	logger      *pct.Logger
	connFactory mysql.ConnectionFactory
	ir          *instance.Repo
}

func NewExplain(logger *pct.Logger, connFactory mysql.ConnectionFactory, ir *instance.Repo) *Explain {
	e := &Explain{
		logger:      logger,
		connFactory: connFactory,
		ir:          ir,
	}
	return e
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (e *Explain) Handle(cmd *proto.Cmd) *proto.Reply {
	// Get explain query
	explainQuery, err := e.getExplainQuery(cmd)
	if err != nil {
		return cmd.Reply(nil, err)
	}

	// The real name of the internal service, e.g. query-mysql-1:
	name := e.getInstanceName(explainQuery.Service, explainQuery.InstanceId)

	e.logger.Info("Running explain", name, cmd)

	// Create connector to MySQL instance
	conn, err := e.createConn(explainQuery.Service, explainQuery.InstanceId)
	if err != nil {
		return cmd.Reply(nil, fmt.Errorf("Unable to create connector for %s: %s", name, err))
	}
	defer conn.Close()

	// Connect to MySQL instance
	if err := conn.Connect(2); err != nil {
		return cmd.Reply(nil, fmt.Errorf("Unable to connect to %s: %s", name, err))
	}

	// Run explain
	explain, err := conn.Explain(explainQuery.Query, explainQuery.Db)
	if err != nil {
		if e.isDMLQuery(explainQuery.Query) {
			newQuery := e.DMLToSelect(explainQuery.Query)
			if newQuery == "" {
				return cmd.Reply(nil, fmt.Errorf("Cannot run EXPLAIN on selected query"))
			}
			explain, err = conn.Explain(e.DMLToSelect(explainQuery.Query), explainQuery.Db)
		}
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("Explain failed for %s: %s", name, err))
		}
	}

	return cmd.Reply(explain)
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (e *Explain) getInstanceName(service string, instanceId uint) (name string) {
	// The real name of the internal service, e.g. query-mysql-1:
	instanceName := e.ir.Name(service, instanceId)
	name = fmt.Sprintf("%s-%s", SERVICE_NAME, instanceName)
	return name
}

func (e *Explain) createConn(service string, instanceId uint) (conn mysql.Connector, err error) {
	// Load the MySQL instance info (DSN, name, etc.).
	mysqlIt := &proto.MySQLInstance{}
	if err = e.ir.Get(service, instanceId, mysqlIt); err != nil {
		return nil, err
	}

	// Create MySQL connection
	conn = e.connFactory.Make(mysqlIt.DSN)

	return conn, nil
}

func (e *Explain) getExplainQuery(cmd *proto.Cmd) (explainQuery *proto.ExplainQuery, err error) {
	if cmd.Data == nil {
		return nil, fmt.Errorf("%s.getExplainQuery:cmd.Data is empty", SERVICE_NAME)
	}

	if err := json.Unmarshal(cmd.Data, &explainQuery); err != nil {
		return nil, fmt.Errorf("%s.getExplainQuery:json.Unmarshal:%s", SERVICE_NAME, err)
	}

	return explainQuery, nil
}

func (e *Explain) isDMLQuery(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	dmlVerbs := []string{"insert", "update", "delete", "replace"}
	for _, verb := range dmlVerbs {
		if strings.HasPrefix(query, verb) {
			return true
		}
	}
	return false
}

/*
  MySQL version prior 5.6.3 cannot run explain on DML commands.
  From the doc: http://dev.mysql.com/doc/refman/5.6/en/explain.html
  "As of MySQL 5.6.3, permitted explainable statements for EXPLAIN are
  SELECT, DELETE, INSERT, REPLACE, and UPDATE.
  Before MySQL 5.6.3, SELECT is the only explainable statement."

  This function converts DML queries to the equivalent SELECT to make
  it able to explain DML queries on older MySQL versions
*/
func (e *Explain) DMLToSelect(query string) string {
	var updateRe = regexp.MustCompile(`(?i)^update\s+(?:low_priority|ignore)?\s*(.*?)\s+set\s+(.*?)(?:\s+where\s+(.*?))?(?:\s+limit\s*[0-9]+(?:\s*,\s*[0-9]+)?)?$`)
	var deleteRe = regexp.MustCompile(`(?i)^delete\s+(.*?)\bfrom\s+(.*?)$`)
	var insertRe = regexp.MustCompile(`(?i)^(?:insert(?:\s+ignore)?|replace)\s+.*?\binto\s+(.*?)\(([^\)]+)\)\s*values?\s*\((.*?)\)\s*(?:\slimit\s|on\s+duplicate\s+key.*)?\s*$`)
	var insertSetRe = regexp.MustCompile(`(?i)(?:insert(?:\s+ignore)?|replace)\s+(?:.*?\binto)\s+(.*?)\s*set\s+(.*?)\s*(?:\blimit\b|on\s+duplicate\s+key.*)?\s*$`)

	m := updateRe.FindStringSubmatch(query)
	// > 2 because we need at least a table name and a list of fields
	if len(m) > 2 {
		return updateToSelect(m)
	}

	m = deleteRe.FindStringSubmatch(query)
	if len(m) > 1 {
		return deleteToSelect(m)
	}

	m = insertRe.FindStringSubmatch(query)
	if len(m) > 2 {
		return insertToSelect(m)
	}

	m = insertSetRe.FindStringSubmatch(query)
	if len(m) > 2 {
		return insertWithSetToSelect(m)
	}

	return ""
}

func updateToSelect(matches []string) string {
	matches = matches[1:]
	matches[0], matches[1] = matches[1], matches[0]
	format := []string{"SELECT %s", " FROM %s", " WHERE %s"}
	result := ""
	for i, match := range matches {
		if match != "" {
			result = result + fmt.Sprintf(format[i], match)
		}
	}
	return result
}

func deleteToSelect(matches []string) string {
	if strings.Index(matches[2], "join") > -1 {
		return fmt.Sprintf("SELECT 1 FROM %s", matches[2])
	}
	return fmt.Sprintf("SELECT * FROM %s", matches[2])
}

func insertToSelect(matches []string) string {
	fields := strings.Split(matches[2], ",")
	values := strings.Split(matches[3], ",")
	if len(fields) == len(values) {
		query := fmt.Sprintf("SELECT * FROM %s WHERE ", matches[1])
		sep := ""
		for i := 0; i < len(fields); i++ {
			query = query + fmt.Sprintf(`%s%s="%s"`, sep, strings.TrimSpace(fields[i]), values[i])
			sep = " and "
		}
		return query
	}
	return fmt.Sprintf("SELECT * FROM %s LIMIT 1", matches[1])
}

func insertWithSetToSelect(matches []string) string {
	return fmt.Sprintf("SELECT * FROM %s WHERE %s", matches[1], strings.Replace(matches[2], ",", " AND ", -1))
}
