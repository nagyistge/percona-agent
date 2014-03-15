package mock

import (
	"github.com/percona/cloud-tools/mysql"
)

type ConnectionFactory struct {
	Conn *NullMySQL
}

func (f *ConnectionFactory) Make(dsn string) mysql.Connector {
	return f.Conn
}
