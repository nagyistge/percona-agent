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

package mysql

import (
	"fmt"
	"os/user"
	"strings"
)

type DSN struct {
	Username     string
	Password     string
	Hostname     string
	Port         string
	Socket       string
	OldPasswords bool
}

const (
	dsnSuffix         = "/?parseTime=true"
	allowOldPasswords = "&allowOldPasswords=true"
)

func (dsn DSN) DSN() (string, error) {
	// Make Sprintf format easier; password doesn't really start with ":".
	if dsn.Password != "" {
		dsn.Password = ":" + dsn.Password
	}

	dsnString := ""
	if dsn.Socket != "" {
		dsnString = fmt.Sprintf("%s%s@unix(%s)",
			dsn.Username,
			dsn.Password,
			dsn.Socket,
		)
	} else if dsn.Hostname != "" {
		if dsn.Port == "" {
			dsn.Port = "3306"
		}
		dsnString = fmt.Sprintf("%s%s@tcp(%s:%s)",
			dsn.Username,
			dsn.Password,
			dsn.Hostname,
			dsn.Port,
		)
	} else {
		user, err := user.Current()
		if err != nil {
			return "", err
		}
		dsnString = fmt.Sprintf("%s@", user.Username)
	}
	dsnString = dsnString + dsnSuffix
	if dsn.OldPasswords {
		dsnString = dsnString + allowOldPasswords
	}
	return dsnString, nil
}

func (dsn DSN) To() string {
	if dsn.Socket != "" {
		return dsn.Socket
	} else if dsn.Hostname != "" {
		if dsn.Port == "" {
			dsn.Port = "3306"
		}
		return fmt.Sprintf(dsn.Hostname + ":" + dsn.Port)
	}
	return "localhost"
}

func (dsn DSN) String() string {
	dsn.Password = "..."
	dsnString, _ := dsn.DSN()
	dsnString = strings.TrimSuffix(dsnString, allowOldPasswords)
	dsnString = strings.TrimSuffix(dsnString, dsnSuffix)
	return dsnString
}
