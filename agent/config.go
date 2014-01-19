package agent

import (
	"encoding/json"
	"errors"
	"github.com/percona/cloud-protocol/proto"
	"io/ioutil"
	"log"
	"os"
)

// Defaults
const (
	API_HOSTNAME = "cloud-api.percona.com"
	CONFIG_FILE  = "/etc/percona/agent.conf"
	LOG_FILE     = "/var/log/percona/agent.log"
	LOG_LEVEL    = "info"
	DATA_DIR     = "/var/spool/percona/agent"
)

type Config struct {
	ApiHostname   string
	ApiKey        string
	AgentUuid     string
	PidFile       string
	LogFile       string
	LogLevel      string
	LogFileOnly   bool
	DataDir       string
	SpoolDataOnly bool
	Links         map[string]string
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

func (c *Config) Apply(d *Config) error {
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
	if d.LogLevel != "" {
		_, ok := proto.LogLevels[d.LogLevel]
		if !ok {
			return errors.New("Invalid log level: " + d.LogLevel)
		}
		c.LogLevel = d.LogLevel
	}
	return nil
}
