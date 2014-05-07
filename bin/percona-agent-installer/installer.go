package main

import (
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"log"
	"os"
	"regexp"
)

type Flags map[string]bool

var portNumberRe = regexp.MustCompile(`\.\d+$`)

type Installer struct {
	term        *Terminal
	basedir     string
	api         pct.APIConnector
	agentConfig *agent.Config
	flags       Flags
	// --
	hostname string
}

func NewInstaller(term *Terminal, basedir string, api pct.APIConnector, agentConfig *agent.Config, flags Flags) *Installer {
	if agentConfig.ApiHostname == "" {
		agentConfig.ApiHostname = agent.DEFAULT_API_HOSTNAME
	}
	hostname, _ := os.Hostname()
	installer := &Installer{
		term:        term,
		basedir:     basedir,
		api:         api,
		agentConfig: agentConfig,
		flags:       flags,
		// --
		hostname: hostname,
	}
	return installer
}

func (i *Installer) Run() error {

	/**
	 * Get the API key.
	 */

	fmt.Printf("API host: %s\n", i.agentConfig.ApiHostname)

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

	if err := i.writeInstances(si, mi); err != nil {
		return fmt.Errorf("Created agent but failed to write service instances: %s", err)
	}

	/**
	 * Get default configs for all services.
	 */

	configs := []proto.AgentConfig{}

	config, err := i.getMmServerConfig(si)
	if err != nil {
		fmt.Println(err)
		fmt.Println("WARNING: cannot server metrics monitor")
	} else {
		configs = append(configs, *config)
	}

	config, err = i.getMmMySQLConfig(mi)
	if err != nil {
		fmt.Println(err)
		fmt.Println("WARNING: cannot start MySQL metrics monitor")
	} else {
		configs = append(configs, *config)
	}

	config, err = i.getSysconfigMySQLConfig(mi)
	if err != nil {
		fmt.Println(err)
		fmt.Println("WARNING: cannot start MySQL configuration monitor")
	} else {
		configs = append(configs, *config)
	}

	// MySQL is local if the server hostname == MySQL hostname without port number.
	if i.hostname == portNumberRe.ReplaceAllLiteralString(mi.Hostname, "") {
		if i.flags["debug"] {
			log.Printf("MySQL is local")
		}
		config, err := i.getQanConfig(mi)
		if err != nil {
			fmt.Println(err)
			fmt.Println("WARNING: cannot start Query Analytics")
		} else {
			configs = append(configs, *config)
		}
	}

	/**
	 * Create agent with initial service configs.
	 */

	agent, err := i.createAgent(configs)
	if err != nil {
		return err
	}
	fmt.Printf("Created agent: uuid=%s\n", agent.Uuid)

	if err := i.writeConfigs(agent, configs); err != nil {
		return fmt.Errorf("Created agent but failed to write its config: %s", err)
	}

	return nil // success
}
