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
	"regexp"
	"sync"

	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/pct"
)

type Repo struct {
	logger      *pct.Logger
	configDir   string
	api         pct.APIConnector
	it          map[string]*proto.Instance
	tree        *proto.Instance
	treeVersion uint
	mux         *sync.RWMutex
}

const (
	INSTANCES_FILE     = "system-tree.json" // relative to Repo.configDir
	INSTANCES_FILEMODE = 0660

	// MYSQL_PREFIX an OS_PREFIX are the instance prefixes we need to validate
	MYSQL_PREFIX = "mysql"
	OS_PREFIX    = "os"
)

var UUID_RE = regexp.MustCompile("^[0-9A-Fa-f]{32}$")

// Creates a new instance repository and returns a pointer to it
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

func isOSInstance(it proto.Instance) bool {
	return it.Prefix == OS_PREFIX
}

func isMySQLInstance(it proto.Instance) bool {
	return it.Prefix == MYSQL_PREFIX
}

func onlyMySQLInsts(slice []proto.Instance) []proto.Instance {
	var justMySQL []proto.Instance
	for _, it := range slice {
		if isMySQLInstance(it) {
			justMySQL = append(justMySQL, it)
		}
	}
	return justMySQL
}

func (r *Repo) configFilePath() string {
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

// Downloads and returns the system tree data from API.
func (r *Repo) downloadInstances() (data []byte, err error) {
	url := r.api.EntryLink("system_tree")
	data = make([]byte, 0)
	if url == "" {
		errMsg := "No 'system_tree' API link registered"
		r.logger.Warn(errMsg)
		return data, errors.New(errMsg)
	}
	r.logger.Info("GET", url)
	code, data, err := r.api.Get(r.api.ApiKey(), url)
	if err != nil {
		return data, err
	}
	if code != http.StatusOK {
		return data, fmt.Errorf("Failed to get system tree, API returned HTTP status code %d", code)
	}
	if data == nil {
		return data, errors.New("API returned an empty system tree data")
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
	// because a bogus config tree is too deep; also, we don't want to limit
	// depth now.
	tovisit := []*proto.Instance{r.tree}
	for {
		count := len(tovisit)
		switch {
		case count == 0:
			return nil
		case count > 0:
			// Pop element
			var inst *proto.Instance
			inst, tovisit = tovisit[len(tovisit)-1], tovisit[:len(tovisit)-1]
			if _, ok := r.it[inst.UUID]; ok {
				// Should this be a Fatal error?
				return fmt.Errorf("Cycle in system tree detected with UUID %s", inst.UUID)
				// TODO: Avoid cycles and keep going?
				// continue
			}
			// Index our instance with its UUID
			r.it[inst.UUID] = inst
			// Queue all our subsystem instances
			for i := range inst.Subsystems {
				tovisit = append(tovisit, &inst.Subsystems[i])
			}
		}
	}
}

// Initializes the instance repository by reading system tree from local file and if not found pulling it from API
func (r *Repo) Init() error {
	r.mux.Lock()
	defer r.mux.Unlock()
	file := r.configFilePath()
	var data []byte
	var err error
	if !pct.FileExists(file) {
		r.logger.Info(fmt.Sprintf("System tree file (%s) does not exist, downloading", file))
		data, err = r.downloadInstances()
		if err != nil {
			r.logger.Error(err)
			return err
		}
	} else {
		r.logger.Debug("Reading " + file)
		data, err = ioutil.ReadFile(file)
		if err != nil {
			r.logger.Error("Could not read system tree file: " + file)
			return err
		}
	}

	r.logger.Debug("Loading system tree data")
	if err := r.loadConfig(data); err != nil {
		r.logger.Error(fmt.Sprintf("Error loading instances config file: %v", err))
		return err
	}
	r.logger.Debug("Saving system tree data to file")
	if err := r.treeToDisk(file); err != nil {
		r.logger.Error(fmt.Sprintf("Error saving system tree to file: %v", err))
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

// Saves system tree to disk
func (r *Repo) treeToDisk(filepath string) error {
	if r.tree == nil {
		// Nothing to save to disk, return inmediatly
		return nil
	}
	data, err := json.Marshal(r.tree)
	if err != nil {
		r.logger.Error(fmt.Sprintf("Error JSON-marshalling system tree: %v", err))
		return err
	}
	return ioutil.WriteFile(filepath, data, INSTANCES_FILEMODE)
}

// Substitute local repo instances with provided tree parameter.
// The method will populate the provided proto.Instance slices with added,
// deleted or updated instances. If writeToDisk = true the tree will be
// dumped to disk if update is successfull.
func (r *Repo) UpdateTree(tree proto.Instance, version uint, writeToDisk bool) error {
	r.logger.Debug("Update:call")
	defer r.logger.Debug("Update:return")

	r.mux.Lock()
	defer r.mux.Unlock()

	return r.updateTree(tree, version, writeToDisk)
}

func (r *Repo) updateTree(tree proto.Instance, version uint, writeToDisk bool) error {
	r.logger.Debug("update:call")
	defer r.logger.Debug("update:return")

	if version <= r.treeVersion {
		err := errors.New("Discarding tree update because its version is lower than current one")
		// Error to the update process but the user should only receive a warning in log
		r.logger.Warn(err)
		return err
	}

	if !isOSInstance(tree) {
		// tree instance root is not an OS instance
		return errors.New("System tree root is not of OS type ('os' prefix)")
	}

	// We need to deep copy the provided tree as it keeps references to
	// proto.Instances in Subsystems slice that are not copied, hence the caller
	// can modify them without our knowledge
	var newTree *proto.Instance
	if err := cloneTree(&tree, &newTree); err != nil {
		return fmt.Errorf("Couldn't clone provided tree: %v", err)
	}
	r.tree = newTree
	r.updateInstanceIndex()
	r.treeVersion = version

	if writeToDisk {
		return r.treeToDisk(r.configFilePath())
	}
	return nil
}

func (r *Repo) Get(uuid string) (proto.Instance, error) {
	r.logger.Debug("Get:call")
	defer r.logger.Debug("Get:return")
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
	if _, ok := r.it[uuid]; !ok {
		r.logger.Info("Downloading system tree from API")
		data, err := r.downloadInstances()
		if err != nil {
			return proto.Instance{}, err
		}
		if err := r.loadConfig(data); err != nil {
			return proto.Instance{}, err
		}
		if err := r.treeToDisk(r.configFilePath()); err != nil {
			return proto.Instance{}, err
		}
	}
	return *r.it[uuid], nil
}

func (r *Repo) valid(uuid string) bool {
	return UUID_RE.MatchString(uuid)
}

// Returns a flat list of instances copies in the tree, notice that these are efectively the subtrees on each instance
func (r *Repo) List() []proto.Instance {
	r.mux.Lock()
	defer r.mux.Unlock()
	var instances []proto.Instance
	for _, inst := range r.it {
		var instCopy *proto.Instance
		if err := cloneTree(&inst, &instCopy); err != nil {
			longErr := fmt.Errorf("Couldn't clone local tree: %v", err)
			r.logger.Error(longErr)
		}
		instances = append(instances, *instCopy)
	}
	return instances
}

// Returns a copy of the tree
func (r *Repo) GetTree() (proto.Instance, error) {
	r.mux.Lock()
	defer r.mux.Unlock()

	if r.tree == nil {
		return proto.Instance{}, errors.New("Repository has no local system tree")
	}

	var newTree *proto.Instance
	if err := cloneTree(&r.tree, &newTree); err != nil {
		longErr := fmt.Errorf("Couldn't clone local tree: %v", err)
		r.logger.Error(longErr)
		return proto.Instance{}, longErr
	}
	return *newTree, nil
}

func (r *Repo) GetTreeVersion() uint {
	return r.treeVersion
}

func (r *Repo) GetMySQLInstances() []proto.Instance {
	return onlyMySQLInsts(r.List())
}
