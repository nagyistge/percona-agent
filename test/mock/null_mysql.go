package mock

import (
	"database/sql"
	"github.com/percona/cloud-tools/mysql"
)

type NullMySQL struct {
	set []mysql.Query
}

func NewNullMySQL() *NullMySQL {
	n := &NullMySQL{
		set: []mysql.Query{},
	}
	return n
}

func (n *NullMySQL) DB() *sql.DB {
	return nil
}

func (n *NullMySQL) DSN() string {
	return "dsn"
}

func (n *NullMySQL) Connect(tries uint) error {
	return nil
}

func (n *NullMySQL) Close() {
	return
}

func (n *NullMySQL) Set(queries []mysql.Query) error {
	for _, q := range queries {
		n.set = append(n.set, q)
	}
	return nil
}

func (n *NullMySQL) GetSet() []mysql.Query {
	return n.set
}

func (n *NullMySQL) Reset() {
	n.set = []mysql.Query{}
}

func (n *NullMySQL) GetGlobalVarString(varName string) string {
	return ""
}
