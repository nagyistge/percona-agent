package config

import (
//	"fmt"
	"os"
	"reflect"
	"io/ioutil"
	"encoding/json"
)

type Config struct {
	ApiKey string `json:"api-key,omitempty"`
	AgentUuid string `json:"agent-uuid,omitempty"`
	SpoolDir string `json:"spool-dir,omitempty"`
	LogFilename string `json:"log-file,omitempty"`
	PidFilename string `json:"pid-file,omitempty"`
	ConfigFilename string `json:"config-file,omitempty"`
}

func (c *Config) ReadFile(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	err = json.Unmarshal(data, c)
	if err != nil {
		return err
	}
	return nil
}

func (c *Config) Apply(d *Config) error {
	cs := reflect.ValueOf(c).Elem()
	ds := reflect.ValueOf(d).Elem()
	for i := 0; i < cs.NumField(); i++ {
		if cs.Field(i).String() == "" && ds.Field(i).String() != "" {
			cs.Field(i).SetString(ds.Field(i).String())
		}
	}
	return nil
}
