package mysql

import (
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
)

type Connection struct {
	dsn  string
	conn *sql.DB
}

func NewConnection(dsn string) *Connection {
	c := &Connection{
		dsn: dsn,
	}
	return c
}

func (c *Connection) Connect() error {
	if c.conn != nil {
		c.conn.Close()
	}

	// Open connection to MySQL but...
	db, err := sql.Open("mysql", c.dsn)
	if err != nil {
		return err
	}

	// ...try to use the connection for real.
	if err := db.Ping(); err != nil {
		// Connection failed.  Wrong username or password?
		db.Close()
		return err
	}

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
