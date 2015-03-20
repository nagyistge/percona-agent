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
	INSTANCES_FILE     = "instances.conf"
	INSTANCES_FILEMODE = 0664
)

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

func (r *Repo) downloadInstances(file string) error {
	// Get instances tree info from API.
	errors.New(fmt.Sprintf("Downloading instance config file from API"))
	url := r.api.EntryLink("insts")
	if url == "" {
		errMsg := "No 'insts' API link registered"
		r.logger.Warn(errMsg)
		return errors.New(errMsg)
	}
	r.logger.Info("GET", url)
	code, data, err := r.api.Get(r.api.ApiKey(), url)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return errors.New(fmt.Sprintf("Failed to get instance config, API returned HTTP status code %d", code))
	}
	if data == nil {
		return errors.New("API returned an empty instance config data")
	}
	return ioutil.WriteFile(file, data, INSTANCES_FILEMODE)
}

//func printIt(slice *[]*proto.Instance) {
//	fmt.Print("Printing slice: ")
//	fmt.Print(slice)
//	//	fmt.Print("Printing slice: ")
//	//	for _, it := range *slice {
//	//		fmt.Print(it.UUID + " ")
//	//	}
//	fmt.Println(" ")
//}

func (r *Repo) updateInstanceIndex() {
	if r.tree != nil {
		// Lets forget about our former index, parse the current tree
		r.it = make(map[string]*proto.Instance)
	}
	// A recursive method is beautiful but unforgiving without tail recursion
	// optimization. Lets do this iterating, we don't want to overflow the stack
	// because of a rogue config tree is deep enough; also, I don't want to limit
	// depth now.
	tovisit := []*proto.Instance{r.tree}
	for {
		count := len(tovisit)
		switch {
		case count == 0:
			return
		case count > 0:
			// Pop element
			var inst *proto.Instance = nil
			inst, tovisit = tovisit[len(tovisit)-1], tovisit[:len(tovisit)-1]
			if _, ok := r.it[inst.UUID]; ok {
				// Should this be a Fatal error?
				r.logger.Error("Cycle in instances tree detected")
				// Avoid cycles
				continue
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

func (r *Repo) loadConfig(file string) error {
	// Caller should lock r.mux
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %v", file, err))
	}
	if err := json.Unmarshal(data, &r.tree); err != nil {
		return errors.New("instance.Repo:json.Unmarshal:" + err.Error())
	}
	r.updateInstanceIndex()
	return nil
}

func (r *Repo) getCfgFilePath() string {
	return filepath.Join(r.configDir, INSTANCES_FILE)
}

// Initializes the instance repository reading instances tree from local file and pulling it from API
func (r *Repo) Init() error {
	r.mux.Lock()
	defer r.mux.Unlock()
	file := r.getCfgFilePath()
	if !pct.FileExists(file) {
		r.logger.Info(fmt.Sprintf("Instance config file does not exist %s, downloading", file))
		if err := r.downloadInstances(file); err != nil {
			r.logger.Error(err)
			return err
		}
	}

	r.logger.Debug("Reading " + file)
	if err := r.loadConfig(file); err != nil {
		r.logger.Error(fmt.Sprintf("Error loading instances config file: %v", err))
		return err
	}
	r.logger.Info("Loaded " + file)
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

// Deep clone data using gob.
func cloneTree(source, target interface{}) error {
	// This will basically binary serialize the data from source and deserialize
	// in target variable creating a fresh copy. This will NOT work with circular
	// data but that is not a problem with proto.Instances
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

// Saves instances tree to disk
func (r *Repo) treeToDisk() error {
	if r.tree == nil {
		// Nothing to save to disk, return inmediatly
		return nil
	}
	data, err := json.Marshal(r.tree)
	if err != nil {
		r.logger.Error(fmt.Sprintf("Error JSON-marshalling instance's tree: %v", err))
		return err
	}
	return ioutil.WriteFile(r.getCfgFilePath(), data, INSTANCES_FILEMODE)
}

// Substitute local repo instances with provided tree parameter.
// The method will popullate the provided proto.Instance slices with added,
// deleted or updated instances. If writeToDisk = true the tree will be
// dumped to disk as the instance config.
func (r *Repo) UpdateTree(tree proto.Instance, added *[]proto.Instance, deleted *[]proto.Instance, updated *[]proto.Instance, writeToDisk bool) error {
	r.logger.Debug("Update:call")
	defer r.logger.Debug("Update:return")

	r.mux.Lock()
	defer r.mux.Unlock()

	return r.updateTree(tree, added, deleted, updated, writeToDisk)
}

func isOSInstance(it proto.Instance) bool {
	// TODO: cloud-protocol should present this string literals as consts
	if it.Type == "OS" && it.Prefix == "os" {
		return true
	}
	return false
}

func isMySQLInst(it proto.Instance) bool {
	// TODO: cloud-protocol should present this string literals as consts
	if it.Type == "MySQL" && it.Prefix == "mysql" {
		return true
	}
	return false
}

func (r *Repo) updateTree(tree proto.Instance, added *[]proto.Instance, deleted *[]proto.Instance, updated *[]proto.Instance, writeToDisk bool) error {
	r.logger.Debug("update:call")
	defer r.logger.Debug("update:return")

	if !isOSInstance(tree) {
		// tree instance root is not an OS instance
		return errors.New("tree instance root is not of OS type")
	}

	oldIt := r.it
	// We need to deep copy the provided tree as it keeps references to
	// proto.Instances in Subsystems slice that are not copied, hence the caller
	// can modify them without our knowledge
	var newTree *proto.Instance
	if err := cloneTree(&tree, &newTree); err != nil {
		return fmt.Errorf("Couldnt clone provided tree: %v", err)
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
			// a parent as modified.
			if !equalInstances(oldIt[uuid], r.it[uuid]) {
				*updated = append(*updated, *(r.it[uuid]))
			}
		} else {
			*deleted = append(*deleted, *(oldIt[uuid]))
		}
	}

	if writeToDisk {
		return r.treeToDisk()
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
		// We do full tree config file downloads, if we can't find an
		// instance UUID download everything and query again
		file := filepath.Join(r.configDir, INSTANCES_FILE)
		if err := r.downloadInstances(file); err != nil {
			return proto.Instance{}, err
		}
		if err := r.loadConfig(file); err != nil {
			return proto.Instance{}, err
		}
		if !r.valid(uuid) {
			return proto.Instance{}, pct.InvalidInstanceError{Id: uuid}
		}
	}
	return *r.it[uuid], nil
}

func (r *Repo) valid(uuid string) bool {
	if _, ok := r.it[uuid]; !ok {
		return false
	}
	return true
}

func (r *Repo) Name(uuid string) string {
	r.mux.Lock()
	defer r.mux.Unlock()
	if instance, ok := r.it[uuid]; ok {
		return instance.Name
	}
	return ""
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
