package main

import (
	"code.google.com/p/gopass"
	"fmt"
	"github.com/percona/cloud-tools/mysql"
	"os/user"
	"path/filepath"
	"strings"
)

func (i *Installer) doMySQL() (dsn mysql.DSN, err error) {
	// XXX Using implicit return
	newMySQLUser, err := i.term.PromptBool("Create new MySQL account for agent?", "Y")
	if err != nil {
		return
	}
	if newMySQLUser {
		fmt.Println("Connect to MySQL to create new MySQL user for agent")
		dsn, err = i.connectMySQL()
		if err != nil {
			return
		}
		fmt.Println("Creating new MySQL user for agent...")
		dsn, err = i.createMySQLUser(dsn)
		if err != nil {
			return
		}
	} else {
		// Let user specify the MySQL account to use for the agent.
		fmt.Println("Use existing MySQL user for agent")
		dsn, err = i.connectMySQL()
		if err != nil {
			return
		}
	}
	fmt.Printf("Agent MySQL user: %s\n", dsn)
	return
}

func (i *Installer) connectMySQL() (mysql.DSN, error) {
	dsn := mysql.DSN{}
	user, _ := user.Current()
	if user != nil {
		dsn.Username = user.Username
	}
	var conn *mysql.Connection

CONNECT_MYSQL:
	for conn == nil {
		username, err := i.term.PromptStringRequired("MySQL username", dsn.Username)
		if err != nil {
			return dsn, err
		}
		dsn.Username = username

		password, err := gopass.GetPass("MySQL password: ")
		if err != nil {
			return dsn, err
		}
		dsn.Password = password

		hostname, err := i.term.PromptStringRequired("MySQL host[:port] or socket file", "localhost")
		if err != nil {
			return dsn, err
		}
		if filepath.IsAbs(hostname) {
			dsn.Socket = hostname
		} else {
			f := strings.Split(hostname, ":")
			dsn.Hostname = f[0]
			if len(f) > 1 {
				dsn.Port = f[1]
			} else {
				dsn.Port = "3306"
			}
		}

		dsnString, err := dsn.DSN()
		if err != nil {
			return dsn, err // shouldn't happen
		}

		fmt.Printf("Connecting to MySQL %s...\n", dsn)
		conn = mysql.NewConnection(dsnString)
		if err := conn.Connect(1); err != nil {
			conn = nil
			fmt.Printf("Error connecting to MySQL %s: %s\n", dsn, err)
			again, err := i.term.PromptBool("Try again?", "Y")
			if err != nil {
				return dsn, err
			}
			if !again {
				return dsn, fmt.Errorf("Failed to connect to MySQL")
			}
			continue CONNECT_MYSQL
		}
		defer conn.Close()

		fmt.Printf("MySQL connection OK\n")
		break
	}
	return dsn, nil
}
