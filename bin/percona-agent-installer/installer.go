package main

import (
	"code.google.com/p/gopass"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/instance"
	mmMySQL "github.com/percona/cloud-tools/mm/mysql"
	mmServer "github.com/percona/cloud-tools/mm/system"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"github.com/percona/cloud-tools/qan"
	sysconfigMySQL "github.com/percona/cloud-tools/sysconfig/mysql"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

type Flags map[string]bool

var portNumberRe = regexp.MustCompile(`\.\d+$`)

type Installer struct {
	term        *Terminal
	api         pct.APIConnector
	agentConfig *agent.Config
	flags       Flags
	// --
	hostname string
}

func NewInstaller(term *Terminal, api pct.APIConnector, agentConfig *agent.Config, flags Flags) *Installer {
	if agentConfig.ApiHostname == "" {
		agentConfig.ApiHostname = agent.DEFAULT_API_HOSTNAME
	}
	hostname, _ := os.Hostname()
	installer := &Installer{
		term:        term,
		api:         api,
		agentConfig: agentConfig,
		flags:       flags,
		// --
		hostname: hostname,
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
		if i.flags["debug"] {
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
	if !i.flags["skip-mysql"] {
		dsn, err := i.doMySQL()
		if err != nil {
			return err
		}
		agentDSN = dsn
	} else {
		fmt.Println("Skip creating MySQL user (-skip-mysql)")
	}

	/**
	 * Create new API resources.
	 */

	si, err := i.createServerInstance()
	if err != nil {
		return err
	}
	fmt.Printf("Created server instance: hostname=%s id=%d\n", si.Hostname, si.Id)

	var mi *proto.MySQLInstance
	if !i.flags["skip-mysql"] {
		mi, err = i.createMySQLInstance(agentDSN)
		if err != nil {
			return err
		}
		fmt.Printf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mi.DSN, mi.Hostname, si.Id)
	} else {
		fmt.Println("Skip creating MySQL instance (-skip-mysql)")
	}

	/**
	 * Get default configs for all services.
	 */

	mmServerConfig, err := i.getMmServerConfig(si)
	if err != nil {
		return err
	}

	mmMySQLConfig, err := i.getMmMySQLConfig(mi)
	if err != nil {
		return err
	}

	sysconfigMySQLConfig, err := i.getSysconfigMySQLConfig(mi)
	if err != nil {
		return err
	}

	// MySQL is local if the server hostname == MySQL hostname without port number.
	mysqlIsLocal := i.hostname == portNumberRe.ReplaceAllLiteralString(i.hostname, "")
	var qanConfig *qan.Config
	if mysqlIsLocal {
		qanConfig, err = i.getQanConfig()
		if err != nil {
			return err
		}
	}

	/**
	 * Create agent with initial service configs.
	 */

	 configs := make(map[string]AgentConfig)
	 configs[""]

	agentUuid, err := i.createAgent(
		mmServerConfig,
		mmMySQLConfig,
		sysconfigMySQLConfig,
		qanConfig,
	)
	if err != nil {
		return err
	}
	fmt.Printf("Created agent: uuid=%s\n", agentUuid)

	return nil
}

// --------------------------------------------------------------------------

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

func (i *Installer) createMySQLUser(dsn mysql.DSN) (mysql.DSN, error) {
	// Same host:port or socket, but different user and pass.
	userDSN := dsn
	userDSN.Username = "percona-agent"
	userDSN.Password = fmt.Sprintf("%p%d", &dsn, rand.Uint32())

	dsnString, _ := dsn.DSN()
	conn := mysql.NewConnection(dsnString)
	if err := conn.Connect(1); err != nil {
		return userDSN, err
	}
	defer conn.Close()

	sql := fmt.Sprintf("GRANT SUPER, PROCESS, USAGE ON *.* TO '%s'@'%%' IDENTIFIED BY '%s'",
		userDSN.Username, userDSN.Password)
	_, err := conn.DB().Exec(sql)
	return userDSN, err
}

func (i *Installer) createServerInstance() (*proto.ServerInstance, error) {
	// POST <api>/instances/server
	si := &proto.ServerInstance{
		Hostname: i.hostname,
	}
	data, err := json.Marshal(si)
	if err != nil {
		return nil, err
	}
	url := pct.URL(i.agentConfig.ApiHostname, "instances", "server")
	if i.flags["debug"] {
		log.Println(url)
	}
	resp, _, err := i.api.Post(i.agentConfig.ApiKey, url, data)
	if i.flags["debug"] {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	// Create new instance, if it already exist then just use it
	// todo: better handling of duplicate instance
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("Failed to create server instance (status code %d)", resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, fmt.Errorf("API did not return location of new server instance")
	}

	// GET <api>/instances/server/id (URI)
	code, data, err := i.api.Get(i.agentConfig.ApiKey, uri)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get new server instance (status code %d)", code)
	}
	if err := json.Unmarshal(data, si); err != nil {
		return nil, err
	}
	return si, nil
}

func (i *Installer) createMySQLInstance(dsn mysql.DSN) (*proto.MySQLInstance, error) {
	// First use instance.Manager to fill in details about the MySQL server.
	dsnString, _ := dsn.DSN()
	mi := &proto.MySQLInstance{
		Hostname: i.hostname,
		DSN:      dsnString,
	}
	if err := instance.GetMySQLInfo(mi); err != nil {
		if i.flags["debug"] {
			log.Printf("err=%s\n", err)
		}
		return nil, err
	}

	// POST <api>/instances/mysql
	data, err := json.Marshal(mi)
	if err != nil {
		return nil, err
	}
	url := pct.URL(i.agentConfig.ApiHostname, "instances", "mysql")
	if i.flags["debug"] {
		log.Println(url)
	}
	resp, _, err := i.api.Post(i.agentConfig.ApiKey, url, data)
	if i.flags["debug"] {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	// Create new instance, if it already exist then just use it
	// todo: better handling of duplicate instance
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("Failed to create MySQL instance (status code %d)", resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, fmt.Errorf("API did not return location of new MySQL instance")
	}

	// GET <api>/instances/mysql/id (URI)
	code, data, err := i.api.Get(i.agentConfig.ApiKey, uri)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get new MySQL instance (status code %d)", code)
	}
	if err := json.Unmarshal(data, mi); err != nil {
		return nil, err
	}
	return mi, nil
}

func (i *Installer) getMmServerConfig(si *proto.ServerInstance) (*mmServer.Config, error) {
	url := i.agentConfig.ApiHostname + "/configs/mm/default-server"
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default server monitor config (status code %d)", code)
	}
	config := &mmServer.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "server"
	config.InstanceId = si.Id
	return config, nil
}

func (i *Installer) getMmMySQLConfig(mi *proto.MySQLInstance) (*mmMySQL.Config, error) {
	url := i.agentConfig.ApiHostname + "/configs/mm/default-mysql"
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL monitor config (status code %d)", code)
	}
	config := &mmMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = mi.Id
	return config, nil
}

func (i *Installer) getSysconfigMySQLConfig(mi *proto.MySQLInstance) (*sysconfigMySQL.Config, error) {
	url := i.agentConfig.ApiHostname + "/configs/sysconfig/default-mysql"
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL sysconfig config (status code %d)", code)
	}
	config := &sysconfigMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = mi.Id
	return config, nil
}

func (i *Installer) getQanConfig(mi *proto.MySQLInstance) (*qan.Config, error) {
	url := i.agentConfig.ApiHostname + "/configs/qan/default"
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default Query Analytics config (status code %d)", code)
	}
	config := &qan.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = si.Id
	return config, nil
}

func (i *Installer) createAgent(configs map[string]proto.AgentConfig) (string, error) {
	// todo
	return "", nil
}
