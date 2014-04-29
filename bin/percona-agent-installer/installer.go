package main

import (
	"code.google.com/p/gopass"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"log"
	"os/user"
	"path/filepath"
	"strings"
)

var (
	ErrApiUnauthorized = errors.New("Unauthorized")
	ErrApiUnknown      = errors.New("Unknown")
)

var requiredEntryLinks = []string{"agents", "instances", "download"}

type Installer struct {
	term        *Terminal
	api         pct.APIConnector
	agentConfig *agent.Config
	// --
	dsn  *mysql.DSN
	conn *mysql.Connection
}

func NewInstaller(term *Terminal, api pct.APIConnector, agentConfig *agent.Config) *Installer {
	if agentConfig.ApiHostname == "" {
		agentConfig.ApiHostname = agent.DEFAULT_API_HOSTNAME
	}
	installer := &Installer{
		term:        term,
		api:         api,
		agentConfig: agentConfig,
	}
	return installer
}

func (i *Installer) Run() error {

	fmt.Printf("API host: %s\n", i.agentConfig.ApiHostname)

	/**
	 * Get the API key.
	 */

	for i.agentConfig.ApiKey == "" {
		apiKey, err := i.term.PromptString("API key", "")
		if err != nil {
			return err
		}
		if apiKey == "" {
			fmt.Println("API key is required, please try again.")
			continue
		}
		i.agentConfig.ApiKey = apiKey
		break
	}

	/**
	 * Verify the API key by pinging the API.
	 */

VERIFY_API_KEY:
	for {
		fmt.Printf("Verifying API key %s...\n", i.agentConfig.ApiKey)
		code, err := pct.Ping(i.agentConfig.ApiHostname, i.agentConfig.ApiKey)
		if err != nil {
			fmt.Printf("Error: %s\n", err)
		}
		if Debug {
			log.Printf("code=%d\n", code)
			log.Printf("err=%s\n", err)
		}
		ok := false
		if code >= 500 {
			fmt.Printf("Sorry, there's an API problem (status code %d). "+
				"Please try to install again. If the problem continues, contact Percona.\n",
				code)
		} else if code == 401 {
			return fmt.Errorf("Access denied.  Check the API key and try again.")
		} else if code >= 300 {
			fmt.Printf("Sorry, there's an installer problem (status code %d). "+
				"Please try to install again. If the problem continues, contact Percona.\n",
				code)
		} else if code != 200 {
			fmt.Printf("Sorry, there's an installer problem (status code %d). "+
				"Please try to install again. If the problem continues, contact Percona.\n",
				code)
		} else {
			ok = true
		}

		if !ok {
			again, err := i.term.PromptBool("Try again?", "Y")
			if err != nil {
				return err
			}
			if !again {
				return fmt.Errorf("Failed to verify API key")
			}
			continue VERIFY_API_KEY
		}

		fmt.Printf("API key %s is OK\n", i.agentConfig.ApiKey)
		break
	}

	/**
	 * Create a MySQL user for the agent, or use an existing one.
	 */

	agentDSN := mysql.DSN{}
	newMySQLUser, err := i.term.PromptBool("Create new MySQL account for agent?", "Y")
	if err != nil {
		if err != nil {
			return err
		}
	}
	if newMySQLUser {
		log.Println("Connect to MySQL to create new MySQL user for agent")
		dsn, err := i.connectMySQL()
		if err != nil {
			return err
		}
		log.Println("Creating new MySQL user for agent...")
		dsn, err = i.createMySQLUser(dsn)
		if err != nil {
			return err
		}
		agentDSN = dsn
	} else {
		// Let user specify the MySQL account to use for the agent.
		log.Println("Use existing MySQL user for agent")
		dsn, err := i.connectMySQL()
		if err != nil {
			return err
		}
		agentDSN = dsn
	}
	log.Println(agentDSN)

	return nil
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
			log.Printf("Error connecting to MySQL %s: %s\n", dsn, err)
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

func (i *Installer) createMySQLUser(dsn mysql.DSN) (mysql.DSN, error) {
	userDSN := mysql.DSN{}

	dsnString, _ := dsn.DSN()
	conn := mysql.NewConnection(dsnString)
	if err := conn.Connect(1); err != nil {
		return userDSN, err
	}
	defer conn.Close()

	return userDSN, nil
}
