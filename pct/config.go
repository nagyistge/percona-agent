package pct

import (
	"encoding/json"
	"os"
)

var ConfigDir string

func WriteConfig(service string, config interface{}) error {
	if ConfigDir == "" {
		return nil // shouldn't happen
	}

	configJson, err := json.Marshal(config)
	if err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	file, err := os.OpenFile(ConfigDir+"/"+service+".conf", flags, 0644)
	if err != nil {
		return err
	}

	if _, err = file.WriteString(string(configJson)); err != nil {
		return err
	}

	return nil
}
