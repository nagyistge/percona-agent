package agent

import (
	"os"
	"reflect"
	"io/ioutil"
	"encoding/json"
)

type Config struct {
	ApiKey string `json:"api-key,omitempty"`
	ApiUrl string `json:"api-url,omitempty"`
	AgentUuid string `json:"agent-uuid,omitempty"`
	SpoolDir string `json:"spool-dir,omitempty"`
	LogFilename string `json:"log-file,omitempty"`
	PidFilename string `json:"pid-file,omitempty"`
	ConfigFilename string `json:"config-file,omitempty"`
}

// Load config from JSON file.
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

/*
 * Apply default config d to missing values in config c.  This uses reflection,
 * so it looks rather complicated, but all it's doing is the Perl equivalent of:
 *   foreach my $key ( keys %d ) {
 *      if ( !$c{$key} ) {
 *         $c{$key} = $d{$key}
 *      }
 *   }
 */
func (c *Config) Apply(d *Config) {
	cs := reflect.ValueOf(c).Elem()
	ds := reflect.ValueOf(d).Elem()
	for i := 0; i < cs.NumField(); i++ {
		if cs.Field(i).String() == "" && ds.Field(i).String() != "" {
			cs.Field(i).SetString(ds.Field(i).String())
		}
	}
}
