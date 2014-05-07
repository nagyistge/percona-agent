package main

import (
	"code.google.com/p/gopass"
	"fmt"
	"github.com/percona/cloud-tools/mysql"
	"os/user"
	"path/filepath"
	"strings"
)

func MakeGrant(dsn mysql.DSN, user string, pass string) string {
	host := "%"
	if dsn.Socket != "" || dsn.Hostname == "localhost"  || dsn.Hostname == "127.0.0.1" {
		host = "localhost"
	}
	return fmt.Sprintf("GRANT SUPER, PROCESS, USAGE ON *.* TO '%s'@'%s' IDENTIFIED BY '%s'", user, host, pass)
}

func (i *Installer) doMySQL() (dsn mysql.DSN, err error) {
	// XXX Using implicit return
	newMySQLUser, err := i.term.PromptBool("Create new MySQL account for agent?", "Y")
	if err != nil {
		return
	}
	for {
		if newMySQLUser {
			fmt.Println("Connect to MySQL to create new MySQL user for agent")
			dsn, err = i.connectMySQL()
			if err == nil {
				fmt.Println("Creating new MySQL user for agent...")
				dsn, err = i.createMySQLUser(dsn)
				if err == nil {
					break // success
				}
			}
			fmt.Println(err)
		} else {
			// Let user specify the MySQL account to use for the agent.
			fmt.Println("Use existing MySQL user for agent")
			dsn, err = i.connectMySQL()
			if err == nil {
				break // success
			}
			fmt.Println(err)
		}

		again, err := i.term.PromptBool("Try again?", "Y")
		if err != nil {
			return dsn, err
		}
		if !again {
			return dsn, fmt.Errorf("Failed to create new MySQL account for agent")
		}
	}
	dsnString, _ := dsn.DSN()
	fmt.Printf("Agent MySQL user: %s\n", dsnString)
	return // implicit
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
