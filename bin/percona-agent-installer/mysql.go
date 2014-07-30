/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

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

package main

import (
	"fmt"
	"github.com/mewpkg/gopass"
	"github.com/percona/percona-agent/mysql"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func MakeGrant(dsn mysql.DSN, user string, pass string) string {
	host := "%"
	if dsn.Socket != "" || dsn.Hostname == "localhost" {
		host = "localhost"
	} else if dsn.Hostname == "127.0.0.1" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO '%s'@'%s' IDENTIFIED BY '%s'", user, host, pass)
}

func (i *Installer) getAgentDSN() (dsn mysql.DSN, err error) {
	// Should we create new MySQL user or use existing one?
	var createUser bool
	if i.flags.Bool["non-interactive"] {
		if !i.flags.Bool["create-mysql-user"] {
			fmt.Println("Skip creating MySQL user (-create-mysql-user=false)")
		}
		createUser = i.flags.Bool["create-mysql-user"]
	} else {
		createUser, err = i.term.PromptBool(
			"Create MySQL user for agent? ('N' to use existing user)",
			"Y",
		)
		if err != nil {
			return dsn, err
		}
	}

	for {
		// Ask user about connection details:
		// user name, password, host name and port, or socket file
		if createUser {
			dsn, err = i.createNewMySQLUser()
		} else {
			dsn, err = i.useExistingMySQLUser()
		}
		if err == nil {
			break // success
		}

		// If something went wrong then print error
		// and ask user if he wants to try again
		fmt.Println(err)

		var again bool
		if i.flags.Bool["non-interactive"] {
			again = false
		} else {
			again, err = i.term.PromptBool("Try again?", "Y")
			if err != nil {
				return dsn, err
			}
		}
		if !again {
			return dsn, fmt.Errorf("Failed to create new MySQL account for agent")
		}
	}
	fmt.Printf("Agent MySQL user: %s\n", dsn.StringWithSuffixes())
	return dsn, nil
}

func (i *Installer) createNewMySQLUser() (dsn mysql.DSN, err error) {
	// Get super user credentials
	fmt.Println("Specify a root/super MySQL user to create a user for the agent")
	superUserDsn, err := i.getDSNFromUser()
	if err != nil {
		return dsn, err
	}

	// Verify super user connection
	err = i.verifyMySQLConnection(superUserDsn)
	if err != nil {
		return dsn, err
	}

	fmt.Println("Creating new MySQL user for agent...")
	// Create new user using super user access
	dsn, err = i.createMySQLUser(superUserDsn)
	if err != nil {
		return dsn, err
	}

	// Verify new DSN
	err = i.verifyMySQLConnection(dsn)
	if err != nil {
		return dsn, err
	}

	return dsn, nil
}

func (i *Installer) useExistingMySQLUser() (dsn mysql.DSN, err error) {
	// Let user specify the MySQL account to use for the agent.
	fmt.Println("Specify the existing MySQL user to use for the agent")
	dsn, err = i.getDSNFromUser()
	if err != nil {
		return dsn, nil
	}

	fmt.Println("Using existing MySQL user for agent...")
	// Verify DSN provided by user
	err = i.verifyMySQLConnection(dsn)
	if err != nil {
		return dsn, err
	}

	return dsn, nil
}

func (i *Installer) getDSNFromUser() (dsn mysql.DSN, err error) {
	if i.flags.Bool["auto-detect-mysql"] {
		autoDSN, err := i.autodetectDSN()
		if err == nil {
			if i.flags.Bool["non-interactive"] {
				fmt.Printf("Using auto-detected DSN\n")
				return *autoDSN, nil
			} else {
				fmt.Printf("Auto detected MySQL connection details: %s\n", autoDSN)
				useAuto, err := i.term.PromptBool("Use auto-detected connection details?", "Y")
				if err != nil {
					return dsn, err
				}
				if useAuto {
					fmt.Printf("Using auto-detected DSN\n")
					return *autoDSN, nil
				}
			}
		}
	}

	if i.flags.Bool["non-interactive"] {
		dsn.Username = i.flags.String["mysql-user"]
		dsn.Password = i.flags.String["mysql-pass"]

		if i.flags.String["mysql-socket"] != "" {
			dsn.Socket = i.flags.String["mysql-socket"]
		} else {
			dsn.Hostname = i.flags.String["mysql-host"]
			dsn.Port = i.flags.String["mysql-port"]
		}
	} else {
		// Ask for username
		username, err := i.term.PromptString("MySQL username", "")
		if err != nil {
			return dsn, err
		}
		dsn.Username = username

		// Ask for password
		var password string
		if i.flags.Bool["plain-passwords"] {
			password, err = i.term.PromptString("MySQL password", "")
		} else {
			password, err = gopass.GetPass("MySQL password: ")
		}
		if err != nil {
			return dsn, err
		}
		dsn.Password = password

		// Ask for hostname / socket path
		hostname, err := i.term.PromptStringRequired(
			"MySQL host[:port] or socket file",
			dsn.To(),
		)
		if err != nil {
			return dsn, err
		}
		if filepath.IsAbs(hostname) {
			dsn.Socket = hostname
			dsn.Hostname = ""
		} else {
			f := strings.Split(hostname, ":")
			dsn.Hostname = f[0]
			if len(f) > 1 {
				dsn.Port = f[1]
			} else {
				dsn.Port = "3306"
			}
			dsn.Socket = ""
		}
	}

	return dsn, nil
}

func (i *Installer) autodetectDSN() (dsn *mysql.DSN, err error) {
	params := []string{}
	if i.flags.String["mysql-defaults-file"] != "" {
		params = append(params, "--defaults-file="+i.flags.String["mysql-defaults-file"])
	}
	// --print-defaults needs to be last param
	params = append(params, "--print-defaults")
	cmd := exec.Command("mysql", params...)
	byteOutput, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	output := string(byteOutput)

	var re *regexp.Regexp
	var result []string // Result of FindStringSubmatch
	dsn = &mysql.DSN{}

	// Note: Since output of mysql --print-defaults
	//       doesn't use quotation marks for values
	//       then we use "space" as a separator
	//       this implies that we are unable to properly detect
	//       e.g. passwords with spaces
	re = regexp.MustCompile("--user=([^ ]+)")
	result = re.FindStringSubmatch(output)
	if result != nil {
		dsn.Username = result[1]
	}

	re = regexp.MustCompile("--password=([^ ]+)")
	result = re.FindStringSubmatch(output)
	if result != nil {
		dsn.Password = result[1]
	}

	re = regexp.MustCompile("--socket=([^ ]+)")
	result = re.FindStringSubmatch(output)
	if result != nil {
		dsn.Socket = result[1]
	}

	if dsn.Socket == "" {
		re = regexp.MustCompile("--host=([^ ]+)")
		result = re.FindStringSubmatch(output)
		if result != nil {
			dsn.Hostname = result[1]
		}

		re = regexp.MustCompile("--port=([^ ]+)")
		result = re.FindStringSubmatch(output)
		if result != nil {
			dsn.Port = result[1]
		}
	}

	return dsn, nil
}

func (i *Installer) verifyMySQLConnection(dsn mysql.DSN) (err error) {
	for {
		fmt.Printf("Testing MySQL connection %s...\n", dsn)
		if err := TestMySQLConnection(dsn); err != nil {
			fmt.Printf("Error connecting to MySQL %s: %s\n", dsn, err)
			var again bool
			if i.flags.Bool["non-interactive"] {
				again = false
			} else {
				again, err = i.term.PromptBool("Try again to connect?", "Y")
				if err != nil {
					return err
				}
			}
			if !again {
				return fmt.Errorf("Failed to connect to MySQL")
			}
			continue // Try again
		}
		break // success
	}

	fmt.Printf("MySQL connection OK\n")
	return nil
}

func TestMySQLConnection(dsn mysql.DSN) error {
	dsnString, err := dsn.DSN()
	if err != nil {
		return err
	}

	conn := mysql.NewConnection(dsnString)
	if err := conn.Connect(1); err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
