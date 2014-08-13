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

package installer

import (
	"fmt"
	_ "github.com/arnehormann/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/bin/percona-agent-installer/term"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"time"
)

var portNumberRe = regexp.MustCompile(`\.\d+$`)

type Flags struct {
	Bool   map[string]bool
	String map[string]string
}

type Installer struct {
	term        *term.Terminal
	basedir     string
	api         pct.APIConnector
	agentConfig *agent.Config
	flags       Flags
	// --
	hostname   string
	defaultDSN mysql.DSN
}

func NewInstaller(terminal *term.Terminal, basedir string, api pct.APIConnector, agentConfig *agent.Config, flags Flags) *Installer {
	if agentConfig.ApiHostname == "" {
		agentConfig.ApiHostname = agent.DEFAULT_API_HOSTNAME
	}
	hostname, _ := os.Hostname()
	defaultDSN := mysql.DSN{
		Username: flags.String["mysql-user"],
		Password: flags.String["mysql-pass"],
		Hostname: flags.String["mysql-host"],
		Port:     flags.String["mysql-port"],
		Socket:   flags.String["mysql-socket"],
	}
	installer := &Installer{
		term:        terminal,
		basedir:     basedir,
		api:         api,
		agentConfig: agentConfig,
		flags:       flags,
		// --
		hostname:   hostname,
		defaultDSN: defaultDSN,
	}
	return installer
}

func (i *Installer) Run() (err error) {
	/**
	 * Get the API key.
	 */
	err = i.InstallerGetApiKey()
	if err != nil {
		return err
	}

	/**
	 * Verify the API key by pinging the API.
	 */
	err = i.VerifyApiKey()
	if err != nil {
		return err
	}

	/**
	 * Create new service instances.
	 */

	// Server instance
	si, err := i.InstallerCreateServerInstance()
	if err != nil {
		return err
	}

	// MySQL instance
	var mi *proto.MySQLInstance
	if i.flags.Bool["mysql"] {
		mi, err = i.InstallerCreateMySQLInstance()
		if err != nil {
			if i.flags.Bool["interactive"] {
				return err
			} else {
				// Automated install, log the error and continue.
				fmt.Printf("Failed to set up MySQL (ignoring because interactive=false): %s\n", err)
			}
		}
	}

	if err = i.writeInstances(si, mi); err != nil {
		return fmt.Errorf("Created agent but failed to write service instances: %s", err)
	}

	/**
	 * Get default configs for all services.
	 */
	configs, err := i.InstallerGetDefaultConfigs(si, mi)
	if err != nil {
		return err
	}

	/**
	 * Create agent with initial service configs.
	 */
	err = i.InstallerCreateAgentWithInitialServiceConfigs(configs)
	if err != nil {
		return err
	}

	return nil // success
}

func (i *Installer) InstallerGetApiKey() error {
	fmt.Printf("API host: %s\n", i.agentConfig.ApiHostname)

	if !i.flags.Bool["interactive"] && i.agentConfig.ApiKey == "" {
		return fmt.Errorf(
			"API key is required, please provide it with -api-key option.\n" +
				"API Key is available at " + i.flags.String["app-host"] + "/api-key",
		)
	} else {
		if i.agentConfig.ApiKey == "" {
			fmt.Printf(
				"No API Key Defined.\n" +
					"Please Enter your API Key, it is available at https://cloud.percona.com/api-key\n",
			)
		}
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
	}

	return nil
}

func (i *Installer) VerifyApiKey() error {
VERIFY_API_KEY:
	for {
		startTime := time.Now()
		fmt.Printf("Verifying API key %s...\n", i.agentConfig.ApiKey)
		code, err := pct.Ping(i.agentConfig.ApiHostname, i.agentConfig.ApiKey)
		elapsedTime := time.Since(startTime)
		elapsedTimeInSeconds := elapsedTime / time.Second

		timeout := false
		if urlErr, ok := err.(*url.Error); ok {
			if netOpErr, ok := urlErr.Err.(*net.OpError); ok && netOpErr.Timeout() {
				timeout = true
			}
		}
		if i.flags.Bool["debug"] {
			log.Printf("code=%d\n", code)
			log.Printf("err=%s\n", err)
		}
		ok := false
		if timeout {
			fmt.Printf(
				"Error: API connection timeout (%ds): %s\n"+
					"Before you try again, please check your connection and DNS configuration.\n",
				elapsedTimeInSeconds,
				err,
			)
		} else if err != nil {
			fmt.Printf("Error: %s\n", err)
		} else if code >= 500 {
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

		// https://jira.percona.com/browse/PCT-617
		// Warn user if request took at least 5s
		if elapsedTimeInSeconds >= 5 {
			fmt.Printf(
				"WARNING: Request to API took %d seconds but it should have taken < 1 second."+
					" There might be a connection problem, or resolving DNS is very slow."+
					" Before continuing, please check the connection and DNS configuration"+
					" as this could prevent percona-agent from installing or working properly."+
					" If running CentOS or Fedora 19+ in a Vagrant VirtualBox, see this bug:\n"+
					" https://github.com/mitchellh/vagrant/issues/1172\n",
				elapsedTimeInSeconds,
			)
			proceed, err := i.term.PromptBool("Continue?", "Y")
			if err != nil {
				return err
			}
			if !proceed {
				return fmt.Errorf("Failed because of slow connection")
			}
		}

		break
	}

	return nil
}

func (i *Installer) InstallerCreateServerInstance() (si *proto.ServerInstance, err error) {
	if i.flags.Bool["create-server-instance"] {
		si, err = i.createServerInstance()
		if err != nil {
			return nil, err
		}
		fmt.Printf("Created server instance: hostname=%s id=%d\n", si.Hostname, si.Id)
	} else {
		fmt.Println("Not creating server instance (-create-server-instance=false)")
	}

	return si, nil
}

func (i *Installer) InstallerCreateMySQLInstance() (mi *proto.MySQLInstance, err error) {
	if i.flags.Bool["create-mysql-instance"] {
		// Get MySQL DSN for agent to use.
		// It is new MySQL user created just for agent
		// or user is asked for existing one.
		// DSN is verified prior returning by connecting to MySQL.
		agentDSN, err := i.getAgentDSN()
		if err != nil {
			return nil, err
		}
		// Create MySQL instance in API.
		mi, err = i.createMySQLInstance(agentDSN)
		if err != nil {
			return nil, err
		}
		fmt.Printf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mi.DSN, mi.Hostname, mi.Id)
	} else {
		fmt.Println("Not creating MySQL instance (-create-mysql-instance=false)")
	}

	return mi, nil
}

func (i *Installer) InstallerGetDefaultConfigs(si *proto.ServerInstance, mi *proto.MySQLInstance) (configs []proto.AgentConfig, err error) {

	if i.flags.Bool["start-services"] {
		// Server metrics monitor
		config, err := i.getMmServerConfig(si)
		if err != nil {
			fmt.Println(err)
			fmt.Println("WARNING: cannot start server metrics monitor")
		} else {
			configs = append(configs, *config)
		}

		if i.flags.Bool["start-mysql-services"] {
			if mi != nil {
				// MySQL metrics tracker
				config, err = i.getMmMySQLConfig(mi)
				if err != nil {
					fmt.Println(err)
					fmt.Println("WARNING: cannot start MySQL metrics monitor")
				} else {
					configs = append(configs, *config)
				}

				// MySQL config tracker
				config, err = i.getSysconfigMySQLConfig(mi)
				if err != nil {
					fmt.Println(err)
					fmt.Println("WARNING: cannot start MySQL configuration monitor")
				} else {
					configs = append(configs, *config)
				}

				// QAN
				// MySQL is local if the server hostname == MySQL hostname without port number.
				if i.hostname == portNumberRe.ReplaceAllLiteralString(mi.Hostname, "") {
					if i.flags.Bool["debug"] {
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
			}
		} else {
			fmt.Println("Not starting MySQL services (-start-mysql-services=false)")
		}
	} else {
		fmt.Println("Not starting default services (-start-services=false)")
	}

	return configs, nil
}

func (i *Installer) InstallerCreateAgentWithInitialServiceConfigs(configs []proto.AgentConfig) (err error) {
	if i.flags.Bool["create-agent"] {
		agent, err := i.createAgent(configs)
		if err != nil {
			return err
		}
		fmt.Printf("Created agent: uuid=%s\n", agent.Uuid)

		if err := i.writeConfigs(agent, configs); err != nil {
			return fmt.Errorf("Created agent but failed to write configs: %s", err)
		}
	} else {
		fmt.Println("Not creating agent (-create-agent=false)")
	}

	return nil
}
