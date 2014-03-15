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

package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/pct"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type Manager struct {
	logger    *pct.Logger
	configDir string
	it        map[string]interface{}
}

func NewManager(logger *pct.Logger, configDir string) *Manager {
	m := &Manager{
		logger:    logger,
		configDir: configDir,
		// --
		it: make(map[string]interface{}),
	}
	return m
}

func (m *Manager) Init() error {
	for service, _ := range proto.ExternalService {
		if err := m.loadInstances(service); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) loadInstances(service string) error {
	files, err := filepath.Glob(m.configDir + "/" + service + "-*.conf")
	if err != nil {
		return err
	}

	for _, file := range files {
		m.logger.Debug("Reading " + file)

		// 0        1       2
		// instance-service-id
		part := strings.Split(filepath.Base(file), "-")
		if len(part) != 3 {
			return errors.New("Invalid instance file name: " + file)
		}
		service := part[1]
		id, err := strconv.ParseUint(part[2], 10, 32)
		if err != nil {
			return err
		}
		if !valid(service, uint(id)) {
			return pct.InvalidServiceInstanceError{Service: service, Id: uint(id)}
		}

		data, err := ioutil.ReadFile(file)
		if err != nil {
			return err
		}

		var info interface{}
		switch service {
		case "server":
			it := &proto.ServerInstance{}
			if err := json.Unmarshal(data, it); err != nil {
				return errors.New("it:Init:json.Unmarshal:" + file + ":" + err.Error())
			}
			info = it
		case "mysql":
			it := &proto.MySQLInstance{}
			if err := json.Unmarshal(data, it); err != nil {
				return errors.New("it:Init:json.Unmarshal:" + file + ":" + err.Error())
			}
			info = it
		default:
			return errors.New(fmt.Sprintf("Invalid service name: %s (%s)", service, file))
		}

		if err := m.Add(service, uint(id), info); err != nil {
			return err
		}
		m.logger.Info("Loaded " + file)
	}
	return nil
}

func (m *Manager) Add(service string, id uint, info interface{}) error {
	if !valid(service, id) {
		return pct.InvalidServiceInstanceError{Service: service, Id: id}
	}
	name := m.Name(service, id)
	if _, ok := m.it[name]; ok {
		return pct.DuplicateServiceInstanceError{Service: service, Id: id}
	}

	file := m.configDir + "/" + name + ".conf"
	m.logger.Info("Writing", file)
	if err := pct.WriteConfig(file, info); err != nil {
		return err
	}

	m.it[name] = info
	return nil
}

func (m *Manager) Get(service string, id uint, info interface{}) error {
	if !valid(service, id) {
		return pct.InvalidServiceInstanceError{Service: service, Id: id}
	}
	name := m.Name(service, id)

	it, ok := m.it[name]
	if !ok {
		return pct.UnknownServiceInstanceError{Service: service, Id: id}
	}

	/**
	 * Yes, we need reflection because "everything in Go is passed by value"
	 * (http://golang.org/doc/faq#Pointers).  When the caller passes a pointer
	 * to a struct (*T) as an interface{} arg, the function receives a new
	 * interface that contains a pointer to the struct.  Therefore, setting
	 * info = it only sets the new interface, not the underlying struct.
	 * The only way to access and change the underlying struct of an interface
	 * is with reflection.  The nex two lines might not make any sense until
	 * you grok reflection; I leave that to you.
	 */
	infoVal := reflect.ValueOf(info).Elem()
	infoVal.Set(reflect.ValueOf(it))

	return nil
}

func (m *Manager) Update(service string, id uint, info []byte) error {
	// todo: API --> agent --> sid.Update()
	// After successful update, agent should restart all services using
	// the instance and reply ok only if all service restart ok.
	return nil
}

func (m *Manager) Remove(id uint) error {
	// todo: API --> agent --> side.Remove()
	// Agent should stop all services using the instance before call this.
	return nil
}

func valid(service string, id uint) bool {
	if _, ok := proto.ExternalService[service]; !ok {
		return false
	}
	if id == 0 {
		return false
	}
	return true
}

func (m * Manager) Name(service string, id uint) string {
	return fmt.Sprintf("%s-%d", service, id)
}
