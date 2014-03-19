package mysql

type ConnectionFactory interface {
	Make(dsn string) Connector
}

type RealConnectionFactory struct {
}

func (f *RealConnectionFactory) Make(dsn string) Connector {
	return NewConnection(dsn)
}
