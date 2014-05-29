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
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/mysql"
	"github.com/percona/percona-agent/pct"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"time"
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
	 * Check for pt-agent, upgrade if found.
	 */

	var ptagentDSN *mysql.DSN
	ptagentUpgrade := false
	ptagentConf := "/root/.pt-agent.conf"
	if pct.FileExists(ptagentConf) {
		fmt.Println("Found pt-agent, upgrading and removing because it is no longer supported...")
		ptagentUpgrade = true

		// Stop pt-agent
		if err := StopPTAgent(); err != nil {
			fmt.Printf("Error stopping pt-agent: %s\n\n", err)
			fmt.Println("WARNING: pt-agent must be stopped before installing percona-agent.  " +
				"Please verify that pt-agent is not running and has been removed from cron.  " +
				"Enter 'Y' to confirm and continue installing percona-agent.")
			ok, err := i.term.PromptBool("pt-agent has stopped?", "N")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("Failed to stop pt-agent")
			}
		}

		// Get its settings (API key, UUID, etc.).
		agent, dsn, err := GetPTAgentSettings(ptagentConf)
		if err != nil {
			return fmt.Errorf("Error upgrading pt-agent: %s", err)
		}
		if agent.ApiKey != "" {
			i.agentConfig.ApiKey = agent.ApiKey
		}
		if agent.AgentUuid != "" {
			i.agentConfig.AgentUuid = agent.AgentUuid
			fmt.Printf("Upgrading pt-agent %s...\n", agent.AgentUuid)
		}
		ptagentDSN = dsn
	}

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
		if i.flags["debug"] {
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

		fmt.Printf("API key %s is OK\n", i.agentConfig.ApiKey)

		if elapsedTimeInSeconds >= 0 {
			fmt.Printf(
				"WARNING: We have detected that request to api took %d second(-s) while usually it shouldn't take more than 1s.\n"+
					"This might be due to connection problems or slow DNS resolution.\n"+
					"Before you continue please check your connection and DNS configuration as this might impact performance of percona-agent.\n"+
					"If you are using CentOS or Fedora 19+ in a vagrant box then you might be interested in this bug report:\n"+
					"https://github.com/mitchellh/vagrant/issues/1172\n",
				elapsedTimeInSeconds,
			)
			proceed, err := i.term.PromptBool("Continue anyway?", "Y")
			if err != nil {
				return err
			}
			if !proceed {
				return fmt.Errorf("Failed because of slow connection")
			}
		}

		break
	}

	var si *proto.ServerInstance
	var mi *proto.MySQLInstance

	/**
	 * Create new service instances.
	 */

	var err error

	if i.flags["create-server-instance"] {
		si, err = i.createServerInstance()
		if err != nil {
			return err
		}
		fmt.Printf("Created server instance: hostname=%s id=%d\n", si.Hostname, si.Id)
	} else {
		fmt.Println("Not creating server instance (-create-server-instance=false)")
	}

	if i.flags["create-mysql-instance"] {
		// Create MySQL user for agent, or using existing one, then verify MySQL connection.
		agentDSN, err := i.doMySQL(ptagentDSN)
		if err != nil {
			return err
		}
		// Create MySQL instance in API.
		mi, err = i.createMySQLInstance(agentDSN)
		if err != nil {
			return err
		}
		fmt.Printf("Created MySQL instance: dsn=%s hostname=%s id=%d\n", mi.DSN, mi.Hostname, si.Id)
	} else {
		fmt.Println("Not creating MySQL instance (-create-mysql-instance=false)")
	}

	if err := i.writeInstances(si, mi); err != nil {
		return fmt.Errorf("Created agent but failed to write service instances: %s", err)
	}

	/**
	 * Get default configs for all services.
	 */

	configs := []proto.AgentConfig{}

	if i.flags["start-services"] {
		// Server metrics monitor
		config, err := i.getMmServerConfig(si)
		if err != nil {
			fmt.Println(err)
			fmt.Println("WARNING: cannot start server metrics monitor")
		} else {
			configs = append(configs, *config)
		}

		if i.flags["start-mysql-services"] {
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
		} else {
			fmt.Println("Not starting MySQL services (-start-mysql-services=false)")
		}
	} else {
		fmt.Println("Not starting default services (-start-services=false)")
	}

	/**
	 * Create agent with initial service configs.
	 */

	if ptagentUpgrade {
		agent, err := i.updateAgent(i.agentConfig.AgentUuid)
		if err != nil {
			return err
		}
		fmt.Println("pt-agent upgraded to percona-agent")
		if err := i.writeConfigs(agent, configs); err != nil {
			return fmt.Errorf("Upgraded pt-agent but failed to write percona-agent configs: %s", err)
		}
	} else if i.flags["create-agent"] {
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

	/**
	 * Remove pt-agent if upgrading.
	 */

	if ptagentUpgrade {
		RemovePTAgent(ptagentConf)
		fmt.Println("pt-agent removed")
	}

	return nil // success
}
