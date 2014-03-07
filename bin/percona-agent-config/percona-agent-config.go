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
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/pct"
	golog "log"
	"os"
	"net/http"
	"bytes"
	"errors"
	"regexp"
	"flag"
	//"os/user"
	//"time"
)

const (                                                                                                                             
        VERSION = "1.0.0"                                                                                                           
)

var apiKey = flag.String("api-key", "", "ApiKey to Percona Cloud Tools")
var apiURL = flag.String("api-url", agent.DEFAULT_API_HOSTNAME, "Api URL to Percona Cloud Tools")

func main() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)

	flag.Parse()
	if *apiKey == "" {
		golog.Fatal("api-key can't be empty")
	}


	// Create default config.
	config := &agent.Config{
		ApiHostname: *apiURL,
	}

	// Overwrite default config with config file.
	configFile := agent.DEFAULT_CONFIG_FILE
	
	golog.Printf("Generating config with following parameters:")
	golog.Printf("ApiKey: %s", *apiKey)
	golog.Printf("Api URL: %s", *apiURL)
	golog.Printf("Config file: %s", configFile)



	if _, err := os.Stat(configFile); err == nil {
		golog.Printf("Config file %s exits, will update it", configFile)
		if err := pct.ReadConfig(configFile, config); err != nil {
			golog.Fatal(err)
		}
	}

	uuid, err := CreateAgent(*apiURL,*apiKey)
	if err != nil {
		golog.Fatal(err)
	}
	
	golog.Printf("Received uuid: %s",uuid)

	config.ApiKey = *apiKey
	config.AgentUuid = uuid

	// Make sure config has everything we need.
	if valid, missing := CheckConfig(config, configFile); !valid {
		golog.Println("Invalid config:")
		for _, m := range missing {
			golog.Printf("  - %s\n", m)
		}
		os.Exit(-1)
	}
	pct.WriteConfig(configFile, config)

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

func CreateAgent(apiURL,apiKey string) (string, error) {
	agentUuid := ""
	hostname, err := os.Hostname()
	if err != nil {
		golog.Printf("Error getting hostname: %s", err)
		return agentUuid, err
	}

	golog.Printf("Using hostname: %s", hostname)
	
        data := proto.AgentData{
                Hostname: hostname,
                Configs: map[string]string{
                        "agent": "{ type: \"agent_inserted\"}",
                        "mm": "{ type: \"mm_inserted\"}",
                },
                Versions: map[string]string{
                        "PerconaAgent": VERSION,
                },
        }

        postData, err := json.Marshal(data)
	if err != nil {
		golog.Fatal(err)
	}

	//postData, err := json.Marshal(data)
	client := &http.Client{}
	req, err := http.NewRequest("POST", "https://"+apiURL+"/agents", bytes.NewReader(postData))
	if err != nil {
		golog.Printf("http.NewRequest error: %s", err)
		return agentUuid, err
	}
	req.Header.Add("X-Percona-API-Key", apiKey)
	resp, err := client.Do(req)

	if err != nil {
		golog.Printf("http POST /agents/ error: %s", err)
		return agentUuid, err
	}

	if resp.StatusCode != 201 {
		golog.Printf("Error: http response code: %d, while expected 201", resp.StatusCode)
		return agentUuid, errors.New(fmt.Sprintf("Return code: %d", resp.StatusCode))
	}

        var validUuid = regexp.MustCompile(`[a-z0-9\-]+$`)
        agentUuid = validUuid.FindString(resp.Header.Get("Location"))
        if agentUuid == "" {
                golog.Printf("No uuid found in the Header Location. It is a shame, we blame Daniel for this.")
		golog.Printf("Full headers: ", resp.Header)
		return agentUuid, errors.New("No uuid found")
        }

	return agentUuid, nil

}

