package mysql

import (
	"fmt"
	"os/user"
	"strings"
)

type DSN struct {
	Username string
	Password string
	Hostname string
	Port     string
	Socket   string
}

const (
	dsnSuffix = "/?parseTime=true"
)

func (dsn DSN) DSN() (string, error) {
	if dsn.Hostname != "" && dsn.Socket != "" {
		return "", fmt.Errorf("Hostname and Socket are mutually exclusive")
	}

	// Make Sprintf format easier; password doesn't really start with ":".
	if dsn.Password != "" {
		dsn.Password = ":" + dsn.Password
	}

	dsnString := ""
	if dsn.Hostname != "" {
		if dsn.Port == "" {
			dsn.Port = "3306"
		}
		dsnString = fmt.Sprintf("%s%s@tcp(%s:%s)",
			dsn.Username,
			dsn.Password,
			dsn.Hostname,
			dsn.Port,
		)
	} else if dsn.Socket != "" {
		dsnString = fmt.Sprintf("%s%s@unix(%s)",
			dsn.Username,
			dsn.Password,
			dsn.Socket,
		)
	} else {
		user, err := user.Current()
		if err != nil {
			return "", err
		}
		dsnString = fmt.Sprintf("%s@", user.Username)
	}
	return dsnString + dsnSuffix, nil
}

func (dsn DSN) String() string {
	dsn.Password = "..."
	dsnString, _ := dsn.DSN()
	dsnString = strings.TrimSuffix(dsnString, dsnSuffix)
	return dsnString
}
