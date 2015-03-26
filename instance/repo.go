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

package instance

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
)

type Repo struct {
	logger         *pct.Logger
	configDir      string
	api            pct.APIConnector
	it             map[string]*proto.Instance
	tree           *proto.Instance
	downloadedInst []byte
	mux            *sync.RWMutex
}

const (
	INSTANCES_FILE     = "instances.conf" // relative to Repo.configDir
	INSTANCES_FILEMODE = 0660

	// Type and prefix of proto.Instances that we need to validate
	// TODO: important, validate the complete tree
	MYSQL_TYPE   = "MySQL"
	MYSQL_PREFIX = "mysql"
	OS_TYPE      = "OS"
	OS_PREFIX    = "os"
)

var UUID_RE, _ = regexp.Compile("^[0-9A-Fa-f]{32}$")

func NewRepo(logger *pct.Logger, configDir string, api pct.APIConnector) *Repo {
	m := &Repo{
		logger:    logger,
		configDir: configDir,
		api:       api,
		// --
		it:   make(map[string]*proto.Instance),
		tree: nil,
		mux:  &sync.RWMutex{},
	}
	return m
}

// Determine if two instances are "equal".
// Instance equality is defined by the equality of all its attributes, plus
// the number of subsystems and their UUIDs.
func equalInstances(inst1, inst2 *proto.Instance) bool {
	if inst1.ParentUUID != inst2.ParentUUID ||
		inst1.UUID != inst2.UUID ||
		inst1.Name != inst2.Name ||
		inst1.Created != inst2.Created ||
		inst1.Deleted != inst2.Deleted ||
		len(inst1.Subsystems) != len(inst2.Subsystems) ||
		!reflect.DeepEqual(inst1.Properties, inst2.Properties) {
		return false
	}
	equals := 0
	for _, it1 := range inst1.Subsystems {
		for _, it2 := range inst2.Subsystems {
			if it1.UUID == it2.UUID {
				equals += 1
				break
			}
		}
	}
	if equals != len(inst1.Subsystems) {
		return false
	}
	return true
}

// Checks if it instance has Type == itType and Prefix == itPrefix
func hasTypePrefix(it proto.Instance, itType string, itPrefix string) bool {
	if it.Type == itType && it.Prefix == itPrefix {
		return true
	}
	return false
}

func isOSInstance(it proto.Instance) bool {
	return hasTypePrefix(it, OS_TYPE, OS_PREFIX)
}

func isMySQLInst(it proto.Instance) bool {
	return hasTypePrefix(it, MYSQL_TYPE, MYSQL_PREFIX)
}

func (r *Repo) getCfgFilePath() string {
	return filepath.Join(r.configDir, INSTANCES_FILE)
}

func (r *Repo) loadConfig(data []byte) error {
	var newTree *proto.Instance
	if err := json.Unmarshal(data, &newTree); err != nil {
		return errors.New("instance.Repo:json.Unmarshal:" + err.Error())
	}
	r.tree = newTree
	return r.updateInstanceIndex()
}

// Downloads and returns the instances tree data from API.
func (r *Repo) downloadInstances() (data []byte, err error) {
	errors.New(fmt.Sprintf("Downloading instance config file from API"))
	url := r.api.EntryLink("insts")
	data = make([]byte, 0)
	if url == "" {
		errMsg := "No 'insts' API link registered"
		r.logger.Warn(errMsg)
		return data, errors.New(errMsg)
	}
	r.logger.Info("GET", url)
	code, data, err := r.api.Get(r.api.ApiKey(), url)
	if err != nil {
		return data, err
	}
	if code != http.StatusOK {
		return data, errors.New(fmt.Sprintf("Failed to get instance config, API returned HTTP status code %d", code))
	}
	if data == nil {
		return data, errors.New("API returned an empty instance config data")
	}
	return data, nil
}

// Updates the local instance UUID index
func (r *Repo) updateInstanceIndex() error {
	if r.tree != nil {
		// Lets forget about our former index, parse the current tree
		r.it = make(map[string]*proto.Instance)
	}
	// A recursive method is beautiful but unforgiving without limits or tail recursion
	// optimization. Lets do this iterating, we don't want to eat all the memory
	// because of a rogue config tree is too deep; also, we don't want to limit
	// depth now.
	tovisit := []*proto.Instance{r.tree}
	for {
		count := len(tovisit)
		switch {
		case count == 0:
			return nil
		case count > 0:
			// Pop element
			var inst *proto.Instance = nil
			inst, tovisit = tovisit[len(tovisit)-1], tovisit[:len(tovisit)-1]
			if _, ok := r.it[inst.UUID]; ok {
				// Should this be a Fatal error?
				return fmt.Errorf("Cycle in instances tree detected with UUID %s", inst.UUID)
				// Avoid cycles
				// continue
			} else {
				// Index our instance with its UUID
				r.it[inst.UUID] = inst
			}
			// Queue all our subsystem instances
			for i, _ := range inst.Subsystems {
				tovisit = append(tovisit, &inst.Subsystems[i])
			}
		}
	}
	return nil
}

// Initializes the instance repository reading instances tree from local file and pulling it from API
func (r *Repo) Init() error {
	r.mux.Lock()
	defer r.mux.Unlock()
	file := r.getCfgFilePath()
	var data []byte
	var err error
	if !pct.FileExists(file) {
		r.logger.Info(fmt.Sprintf("Instance config file (%s) does not exist, downloading", file))
		data, err = r.downloadInstances()
		if err != nil {
			r.logger.Error(err)
			return err
		}
	} else {
		r.logger.Debug("Reading " + file)
		data, err = ioutil.ReadFile(file)
		if err != nil {
			r.logger.Error("Could not read instance config file: " + file)
			return err
		}
	}

	r.logger.Debug("Loading instance config data")
	if err := r.loadConfig(data); err != nil {
		r.logger.Error(fmt.Sprintf("Error loading instances config file: %v", err))
		return err
	}
	r.logger.Debug("Saving instance config data to file")
	if err := r.treeToDisk(file); err != nil {
		r.logger.Error(fmt.Sprintf("Error saving instance config tree to file: %v", err))
		return err
	}
	r.logger.Info("Loaded " + file)
	return nil
}

// Deep clone data using gob.
func cloneTree(source, target interface{}) error {
	// This will basically binary serialize the data from source and deserialize
	// in target variable creating a fresh copy. This will NOT work with circular
	// data but that is not a problem with proto.Instances as they don't hold
	// circular references.
	// Pulled from: https://groups.google.com/forum/#!topic/golang-nuts/vK6P0dmQI84
	// TODO: write a specific clone function for proto.Instance?
	buff := new(bytes.Buffer)
	enc := gob.NewEncoder(buff)
	dec := gob.NewDecoder(buff)
	if err := enc.Encode(source); err != nil {
		return err
	}
	if err := dec.Decode(target); err != nil {
		return err
	}
	return nil
}

// Returns a copy of the instances tree
func (r *Repo) Instances() proto.Instance {
	r.mux.Lock()
	defer r.mux.Unlock()

	var newTree *proto.Instance = nil
	cloneTree(r.tree, &newTree)
	return *newTree
}

// Saves instances tree to disk
func (r *Repo) treeToDisk(filepath string) error {
	if r.tree == nil {
		// Nothing to save to disk, return inmediatly
		return nil
	}
	data, err := json.Marshal(r.tree)
	if err != nil {
		r.logger.Error(fmt.Sprintf("Error JSON-marshalling instance's tree: %v", err))
		return err
	}
	return ioutil.WriteFile(filepath, data, INSTANCES_FILEMODE)
}

// Substitute local repo instances with provided tree parameter.
// The method will populate the provided proto.Instance slices with added,
// deleted or updated instances. If writeToDisk = true the tree will be
// dumped to disk if update is successfull.
func (r *Repo) UpdateTree(tree proto.Instance, added *[]proto.Instance, deleted *[]proto.Instance, updated *[]proto.Instance, writeToDisk bool) error {
	r.logger.Debug("Update:call")
	defer r.logger.Debug("Update:return")

	r.mux.Lock()
	defer r.mux.Unlock()

	return r.updateTree(tree, added, deleted, updated, writeToDisk)
}

func (r *Repo) updateTree(tree proto.Instance, added *[]proto.Instance, deleted *[]proto.Instance, updated *[]proto.Instance, writeToDisk bool) error {
	r.logger.Debug("update:call")
	defer r.logger.Debug("update:return")

	if !isOSInstance(tree) {
		// tree instance root is not an OS instance
		return errors.New("Tree instance root is not of 'OS' type and 'os' prefix")
	}

	oldIt := r.it
	// We need to deep copy the provided tree as it keeps references to
	// proto.Instances in Subsystems slice that are not copied, hence the caller
	// can modify them without our knowledge
	var newTree *proto.Instance
	if err := cloneTree(&tree, &newTree); err != nil {
		return fmt.Errorf("Couldn't clone provided tree: %v", err)
	}
	r.tree = newTree
	r.updateInstanceIndex()

	// Find out what new instances are not in old r.it
	for _, it := range r.it {
		if _, ok := oldIt[it.UUID]; !ok {
			*added = append(*added, *it)
		}
	}

	// Find out what instances were updated or deleted
	for uuid, _ := range oldIt {
		// Does it exist in new tree?
		if _, ok := r.it[uuid]; ok {
			// Is it the same as the former instance?
			// We use custom compare method instead of using DeepEquals
			// on the instances as they include references to child instances;
			// a change in a child instance means DeepEquals will detect that
			// a parent was also modified.
			if !equalInstances(oldIt[uuid], r.it[uuid]) {
				*updated = append(*updated, *(r.it[uuid]))
			}
		} else {
			*deleted = append(*deleted, *(oldIt[uuid]))
		}
	}

	if writeToDisk {
		return r.treeToDisk(r.getCfgFilePath())
	}
	return nil
}

func (r *Repo) Get(uuid string) (proto.Instance, error) {
	//r.logger.Debug("Get:call")
	//defer r.logger.Debug("Get:return")
	r.mux.Lock()
	defer r.mux.Unlock()
	return r.get(uuid)
}

func (r *Repo) get(uuid string) (proto.Instance, error) {
	r.logger.Debug("get:call")
	defer r.logger.Debug("get:return")

	if !r.valid(uuid) {
		return proto.Instance{}, pct.InvalidInstanceError{Id: uuid}
	}

	// We do full tree syncs, if we can't find an instance UUID download
	// everything and query again.
	// TODO: implement instances (tree branches) checksums to avoid downloading
	// unmodified trees
	if _, ok := r.it[uuid]; !ok {
		data, err := r.downloadInstances()
		if err != nil {
			return proto.Instance{}, err
		}
		if err := r.loadConfig(data); err != nil {
			return proto.Instance{}, err
		}
		if err := r.treeToDisk(r.getCfgFilePath()); err != nil {
			return proto.Instance{}, err
		}
	}
	return *r.it[uuid], nil
}

func (r *Repo) valid(uuid string) bool {
	return UUID_RE.MatchString(uuid)
}

func (r *Repo) List() []proto.Instance {
	r.mux.Lock()
	defer r.mux.Unlock()
	instances := make([]proto.Instance, 0)
	for _, inst := range r.it {
		instances = append(instances, *inst)
	}
	return instances
}
