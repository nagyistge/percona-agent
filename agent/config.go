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

package agent

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
)

const (
	DEFAULT_CONFIG_FILE  = "/etc/percona/agent.conf"
	DEFAULT_API_HOSTNAME = "cloud-api.percona.com"
	DEFAULT_PID_FILE     = ""
)

type Config struct {
	AgentUuid   string
	ApiHostname string
	ApiKey      string
	PidFile     string
	// Internal
	Links     map[string]string
	Enable    []string
	Disable   []string
}

// Load config from JSON file.
func LoadConfig(dir string) (*Config, error) {
	data, err := ioutil.ReadFile(dir + "/agent.conf")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	config := &Config{}
	if err = json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	if config.ApiHostname == "" {
		config.ApiHostname = DEFAULT_API_HOSTNAME
	}

	return config, nil
}

// Write config into  JSON file.
func WriteConfig(file string, cur *Config) error {

	b, err := json.MarshalIndent(cur, "", "    ")
	if err != nil {
		log.Fatalln(err)
	}

	err = ioutil.WriteFile(file, b, 0644)
	if err != nil {
		log.Fatalln(err)
	}

	return nil
}
