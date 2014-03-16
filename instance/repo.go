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
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

type Repo struct {
	logger    *pct.Logger
	configDir string
	it        map[string]interface{}
	mux       *sync.RWMutex
}

func NewRepo(logger *pct.Logger, configDir string) *Repo {
	m := &Repo{
		logger:    logger,
		configDir: configDir,
		// --
		it:  make(map[string]interface{}),
		mux: &sync.RWMutex{},
	}
	return m
}

func (r *Repo) Init() error {
	for service, _ := range proto.ExternalService {
		if err := r.loadInstances(service); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) loadInstances(service string) error {
	files, err := filepath.Glob(r.configDir + "/" + service + "-*.conf")
	if err != nil {
		return err
	}

	for _, file := range files {
		r.logger.Debug("Reading " + file)

		// 0       1
		// service-id
		part := strings.Split(strings.TrimSuffix(filepath.Base(file), ".conf"), "-")
		if len(part) != 2 {
			return errors.New("Invalid instance file name: " + file)
		}
		service := part[0]
		id, err := strconv.ParseUint(part[1], 10, 32)
		if err != nil {
			return err
		}
		if !valid(service, uint(id)) {
			return pct.InvalidServiceInstanceError{Service: service, Id: uint(id)}
		}

		data, err := ioutil.ReadFile(file)
		if err != nil {
			return errors.New(file + ":" + err.Error())
		}

		if err := r.Add(service, uint(id), data, false); err != nil {
			return errors.New(file + ":" + err.Error())
		}

		r.logger.Info("Loaded " + file)
	}
	return nil
}

func (r *Repo) Add(service string, id uint, data []byte, writeToDisk bool) error {
	if !valid(service, id) {
		return pct.InvalidServiceInstanceError{Service: service, Id: id}
	}

	var info interface{}
	switch service {
	case "server":
		it := &proto.ServerInstance{}
		if err := json.Unmarshal(data, it); err != nil {
			return errors.New("instance.Repo:json.Unmarshal:" + err.Error())
		}
		info = it
	case "mysql":
		it := &proto.MySQLInstance{}
		if err := json.Unmarshal(data, it); err != nil {
			return errors.New("instance.Repo:json.Unmarshal:" + err.Error())
		}
		info = it
	default:
		return errors.New(fmt.Sprintf("Invalid service name: %s", service))
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	name := r.Name(service, id)
	if _, ok := r.it[name]; ok {
		return pct.DuplicateServiceInstanceError{Service: service, Id: id}
	}

	if writeToDisk {
		file := r.configDir + "/" + name + ".conf"
		r.logger.Info("Writing", file)
		if err := pct.WriteConfig(file, info); err != nil {
			return err
		}
	}

	r.it[name] = info
	return nil
}

func (r *Repo) Get(service string, id uint, info interface{}) error {
	if reflect.ValueOf(info).Kind() != reflect.Ptr {
		log.Fatal("info arg is not a pointer; need &T{}")
	}

	if !valid(service, id) {
		return pct.InvalidServiceInstanceError{Service: service, Id: id}
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	name := r.Name(service, id)
	it, ok := r.it[name]
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
	infoVal.Set(reflect.ValueOf(it).Elem())

	return nil
}

func (r *Repo) Remove(service string, id uint) error {
	// todo: API --> agent --> side.Remove()
	// Agent should stop all services using the instance before call this.
	if !valid(service, id) {
		return pct.InvalidServiceInstanceError{Service: service, Id: id}
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	name := r.Name(service, id)
	if _, ok := r.it[name]; !ok {
		return pct.UnknownServiceInstanceError{Service: service, Id: id}
	}

	file := r.configDir + "/" + name + ".conf"
	r.logger.Info("Removing", file)
	if err := os.Remove(file); err != nil {
		return err
	}

	delete(r.it, name)
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

func (r *Repo) Name(service string, id uint) string {
	return fmt.Sprintf("%s-%d", service, id)
}
