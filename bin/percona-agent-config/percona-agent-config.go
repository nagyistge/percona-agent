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
	"github.com/percona/cloud-tools/agent"
	"log"
	"os"
	//"os/user"
	//"time"
)


func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Parse command line.
	var arg string

	// Create default config.
	config := &agent.Config{
		ApiHostname: agent.API_HOSTNAME,
		LogFile:     agent.LOG_FILE,
		LogLevel:    agent.LOG_LEVEL,
		DataDir:     agent.DATA_DIR,
	}

	// Overwrite default config with config file.
	configFile := arg
	if configFile == "" {
		configFile = agent.CONFIG_FILE
	}
	if err := config.Apply(agent.LoadConfig(configFile)); err != nil {
		log.Fatal(err)
	}

	config.ApiKey = "2323"
	config.AgentUuid = "56556"

	// Make sure config has everything we need.
	if valid, missing := CheckConfig(config, configFile); !valid {
		log.Println("Invalid config:")
		for _, m := range missing {
			log.Printf("  - %s\n", m)
		}
		os.Exit(-1)
	}
	agent.WriteConfig(configFile, config)

}

func CheckConfig(config *agent.Config, configFile string) (bool, []string) {
	isValid := true
	missing := []string{}

	if config.ApiHostname == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No ApiHostname in %s\n", configFile)) // shouldn't happen
	}
	if config.ApiKey == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No ApiKey in %s\n", configFile))
	}
	if config.AgentUuid == "" {
		isValid = false
		missing = append(missing, fmt.Sprintf("No AgentUuid in %s\n", configFile))
	}
	return isValid, missing
}
