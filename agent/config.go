package agent

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"log"
)

type Config struct {
	ApiHostname    string
	ApiKey         string
	AgentUuid      string
	LogFile        string
	PidFile        string
	LogFileOnly    bool
}

// Load config from JSON file.
func LoadConfig(file string) *Config {
	config := new(Config)
	data, err := ioutil.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalln(err)
		}
	} else {
		if err = json.Unmarshal(data, config); err != nil {
			log.Fatalln(err)
		}
	}
	return config
}

func (c *Config) Apply(d *Config) {
	if d.ApiHostname != "" {
		c.ApiHostname = d.ApiHostname
	}
	c.ApiKey = d.ApiKey
	c.AgentUuid = d.AgentUuid
	c.PidFile = d.PidFile
	c.LogFileOnly = d.LogFileOnly
	if d.LogFile != "" {
		c.LogFile = d.LogFile
	}
}
