package mock

import (
	"github.com/percona/cloud-tools/mysql"
)

type NullMySQL struct {
	dsn  string
	set []mysql.Query
}

func NewNullMySQL() *NullMySQL {
	n := &NullMySQL{
		set: []mysql.Query{},
	}
	return n
}

func (n *NullMySQL) Connect(dsn string) error {
	n.dsn = dsn
	return nil
}

func (n *NullMySQL) Set(queries []mysql.Query) error {
	n.set = queries
	return nil
}

func (n *NullMySQL) GetSet() []mysql.Query {
	return n.set
}

func (n *NullMySQL) Reset() {
	n.dsn = ""
	n.set = []mysql.Query{}
}
