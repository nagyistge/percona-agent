package mysql

import (
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
)

type Connector interface {
	Connect(dsn string) error
	Set([]Query) error
}

type Connection struct {
	dsn  string
	conn *sql.DB
}

func NewConnection() *Connection {
	c := &Connection{}
	return c
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