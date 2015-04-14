/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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

package pct

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

const (
	DEFAULT_BASEDIR    = "/usr/local/percona/percona-agent"
	CONFIG_FILE_SUFFIX = ".conf"
	// Relative to Basedir.path:
	CONFIG_DIR   = "config"
	DATA_DIR     = "data"
	BIN_DIR      = "bin"
	TRASH_DIR    = "trash"
	START_LOCK   = "start.lock"
	START_SCRIPT = "start.sh"
)

type basedir struct {
	path      string
	configDir string
	dataDir   string
	binDir    string
	trashDir  string
}

var Basedir basedir

func (b *basedir) Init(path string) error {
	var err error
	b.path, err = filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := MakeDir(b.path); err != nil && !os.IsExist(err) {
		return err
	}
	b.configDir = filepath.Join(b.path, CONFIG_DIR)
	if err := MakeDir(b.configDir); err != nil && !os.IsExist(err) {
		return err
	}
	if err := os.Chmod(b.configDir, 0700); err != nil {
		return err
	}

	b.dataDir = filepath.Join(b.path, DATA_DIR)
	if err := MakeDir(b.dataDir); err != nil && !os.IsExist(err) {
		return err
	}

	b.binDir = filepath.Join(b.path, BIN_DIR)
	if err := MakeDir(b.binDir); err != nil && !os.IsExist(err) {
		return err
	}

	b.trashDir = filepath.Join(b.path, TRASH_DIR)
	if err := MakeDir(b.trashDir); err != nil && !os.IsExist(err) {
		return err
	}

	return nil
}

func (b *basedir) Path() string {
	return b.path
}

func (b *basedir) Dir(service string) string {
	switch service {
	case "config":
		return b.configDir
	case "data":
		return b.dataDir
	case "bin":
		return b.binDir
	case "trash":
		return b.trashDir
	default:
		log.Panic("Invalid service: " + service)
	}
	return ""
}

func (b *basedir) ConfigFile(service string) string {
	return filepath.Join(b.configDir, service+CONFIG_FILE_SUFFIX)
}

func (b *basedir) InstanceConfigFile(service, UUID string) string {
	return filepath.Join(b.configDir, service+"-"+UUID+CONFIG_FILE_SUFFIX)
}

type ConfigIterator struct {
	current int
	files   []string
}

func (it *ConfigIterator) Next() bool {
	it.current++
	if it.current >= len(it.files) {
		return false
	}
	return true
}

func (it *ConfigIterator) Read(v interface{}) error {
	configFile := it.files[it.current]
	data, err := ioutil.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		// There's an error and it's not "file not found"
		return fmt.Errorf("Error reading config file (%s): %v", configFile, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("Config file is empty: %s", configFile)
	}
	err = json.Unmarshal(data, &v)
	if err != nil {
		return fmt.Errorf("Error unmarshalling config file (%s): %v", configFile, err)
	}
	return nil
}

func (b *basedir) NewConfigIterator(service string) (*ConfigIterator, error) {
	files, err := filepath.Glob(b.configDir + "/" + service + "-*" + CONFIG_FILE_SUFFIX)
	if err != nil {
		return nil, fmt.Errorf("Could not get %s config files: %v", service, err)
	}
	return &ConfigIterator{files: files, current: -1}, nil
}

func (b *basedir) ReadConfig(name string, v interface{}) error {
	data, err := ioutil.ReadFile(b.ConfigFile(name))
	if err != nil && !os.IsNotExist(err) {
		// There's an error and it's not "file not found".
		return err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &v)
	}
	return err
}

func (b *basedir) ReadInstanceConfig(service, UUID string, config interface{}) error {
	if err := b.ReadConfig(b.InstanceConfigFile(service, UUID), config); err != nil {
		return fmt.Errorf("Could not read config file: %v", err)
	}
	return nil
}

func (b *basedir) WriteConfig(name string, config interface{}) error {
	return b.writeFile(b.ConfigFile(name), config)
}

// Given a service string and a config this method will serialize the config
// to JSON and store the result in a file with composite name <service>-<uuid>.conf
func (b *basedir) WriteInstanceConfig(service, UUID string, config interface{}) error {
	if err := b.writeFile(b.InstanceConfigFile(service, UUID), config); err != nil {
		return fmt.Errorf("Could not store config file: %v", err)
	}
	return nil
}

// writeFile writes a config to a filePath
func (b *basedir) writeFile(filePath string, config interface{}) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filePath, data, 0600)
}

func (b *basedir) WriteConfigString(service, config string) error {
	return ioutil.WriteFile(b.ConfigFile(service), []byte(config), 0600)
}

func (b *basedir) RemoveConfig(service string) error {
	return RemoveFile(b.ConfigFile(service))
}

func (b *basedir) RemoveInstanceConfig(service, UUID string) error {
	return RemoveFile(b.InstanceConfigFile(service, UUID))
}

func (b *basedir) File(file string) string {
	switch file {
	case "start-lock":
		file = START_LOCK
	case "start-script":
		file = START_SCRIPT
	default:
		log.Panicf("Unknown basedir file: %s", file)
	}
	return filepath.Join(b.Path(), file)
}
