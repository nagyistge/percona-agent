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
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/pct"
)

type Repo struct {
	logger      *pct.Logger
	configDir   string
	it          map[string]*proto.Instance
	tree        *proto.Instance
	treeVersion uint
	mux         *sync.RWMutex
}

const (
	SYSTEM_TREE_FILE     = "system-tree.json" // relative to Repo.configDir
	SYSTEM_TREE_FILEMODE = 0660

	// Instance prefixes used to validate
	MYSQL_PREFIX = "mysql"
	OS_PREFIX    = "os"
)

var UUID_RE = regexp.MustCompile("^[0-9A-Fa-f]{32}$")

// Creates a new instance repository and returns a pointer to it
func NewRepo(logger *pct.Logger, configDir string) *Repo {
	m := &Repo{
		logger:    logger,
		configDir: configDir,
		// --
		it:   make(map[string]*proto.Instance),
		tree: nil,
		mux:  &sync.RWMutex{},
	}
	return m
}

/* To be able to do a lexicographically sort of proto.Instance slice by UUID */

type ByUUID []proto.Instance

func (s ByUUID) Len() int {
	return len(s)
}

func (s ByUUID) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByUUID) Less(i, j int) bool {
	return s[i].UUID < s[j].UUID
}

// Function used by all components to check if and instance is an OS one
func IsOSInstance(it proto.Instance) bool {
	return it.Prefix == OS_PREFIX
}

// Function used by all components to check if and instance is a MySQL one
func IsMySQLInstance(it proto.Instance) bool {
	return it.Prefix == MYSQL_PREFIX
}

// Returns a new slice containing only the MySQL instances present in the slice parameter
func onlyMySQLInsts(slice []proto.Instance) []proto.Instance {
	var onlyMySQL []proto.Instance
	for _, it := range slice {
		if IsMySQLInstance(it) {
			onlyMySQL = append(onlyMySQL, it)
		}
	}
	return onlyMySQL
}

func (r *Repo) systemTreeFilePath() string {
	return filepath.Join(r.configDir, SYSTEM_TREE_FILE)
}

func (r *Repo) loadSystemTree(data []byte) error {
	r.logger.Debug("loadSystemTree:call")
	defer r.logger.Debug("loadSystemTree:return")
	var newTree *proto.Instance
	if err := json.Unmarshal(data, &newTree); err != nil {
		return errors.New("instance.Repo:json.Unmarshal:" + err.Error())
	}
	r.tree = newTree
	return r.updateInstanceIndex()
}

// Updates the local instance UUID index
func (r *Repo) updateInstanceIndex() error {
	r.logger.Debug("updateInstanceIndex:call")
	defer r.logger.Debug("updateInstanceIndex:return")
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

// Initializes the instance repository by reading system tree from local file
func (r *Repo) Init() (err error) {
	r.logger.Debug("Init:call")
	defer r.logger.Debug("Init:return")
	r.mux.Lock()
	defer r.mux.Unlock()
	file := r.systemTreeFilePath()
	var data []byte

	if !pct.FileExists(file) {
		r.logger.Error(fmt.Sprintf("System tree file (%s) does not exist", file))
		return pct.ErrNoSystemTree
	}

	r.logger.Debug("Reading " + file)
	data, err = ioutil.ReadFile(file)
	if err != nil {
		r.logger.Error("Could not read system tree file: " + file)
		return err
	}

	if err = r.loadSystemTree(data); err != nil {
		r.logger.Error(fmt.Sprintf("Error loading instances config file: %v", err))
		return err
	}
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
	return ioutil.WriteFile(filepath, data, SYSTEM_TREE_FILEMODE)
}

// Substitute local repo instances with provided tree parameter.
// The method will populate the provided proto.Instance slices with added,
// deleted or updated instances. The tree will be saved to disk if update is successfull.
func (r *Repo) UpdateSystemTree(tree proto.Instance, version uint) error {
	r.logger.Debug("UpdateSystemTree:call")
	defer r.logger.Debug("UpdateSystemTree:return")
	r.mux.Lock()
	defer r.mux.Unlock()

	if version <= r.treeVersion && r.tree != nil {
		err := fmt.Errorf("Discarding tree version %d update because its version is lower or equal to current %d version", version, r.treeVersion)
		// Error out but user should only receive a warning in log
		r.logger.Warn(err)
		return err
	}

	if !IsOSInstance(tree) {
		// tree instance root is not an OS instance
		return errors.New("System tree root is not of OS type ('os' prefix)")
	}

	// We need to deep copy the provided tree as it keeps references to
	// proto.Instances in Subsystems slice that are not copied, hence the caller
	// can modify them without our knowledge
	var newTree *proto.Instance
	if err := cloneTree(&tree, &newTree); err != nil {
		return fmt.Errorf("Couldn't clone provided system tree: %v", err)
	}
	r.tree = newTree
	r.updateInstanceIndex()
	r.treeVersion = version

	if err := r.treeToDisk(r.systemTreeFilePath()); err != nil {
		return err
	}
	return nil
}

func (r *Repo) Get(uuid string) (proto.Instance, error) {
	r.logger.Debug("Get:call")
	defer r.logger.Debug("Get:return")
	r.mux.Lock()
	defer r.mux.Unlock()

	newInstance := proto.Instance{}
	got, err := r.get(uuid)
	if err != nil {
		return newInstance, err
	}
	// Every instance is a branch, we need to copy
	if err := cloneTree(got, &newInstance); err != nil {
		return newInstance, err
	}

	return newInstance, nil
}

func (r *Repo) get(uuid string) (*proto.Instance, error) {
	r.logger.Debug("get:call")
	defer r.logger.Debug("get:return")

	if !r.valid(uuid) {
		return nil, pct.InvalidInstanceError{UUID: uuid}
	}

	if _, ok := r.it[uuid]; !ok {
		return nil, fmt.Errorf("Instance UUID %s not found in local system tree", uuid)
	}
	return r.it[uuid], nil
}

func (r *Repo) valid(uuid string) bool {
	return UUID_RE.MatchString(uuid)
}

// Returns a flat list of instances copies in the tree, notice that these are efectively the subtrees on each instance
// The list will be lexicographically ordered by instance UUID
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
	sort.Sort(ByUUID(instances))
	return instances
}

// Returns a copy of the tree
func (r *Repo) GetSystemTree() (proto.Instance, error) {
	r.logger.Debug("GetSystemTree:call")
	defer r.logger.Debug("GetSystemTree:return")
	r.mux.Lock()
	defer r.mux.Unlock()

	if r.tree == nil {
		return proto.Instance{}, errors.New("Repository has no local system tree")
	}

	newTree := proto.Instance{}
	if err := cloneTree(&r.tree, &newTree); err != nil {
		longErr := fmt.Errorf("Couldn't clone local tree: %v", err)
		r.logger.Error(longErr)
		return proto.Instance{}, longErr
	}
	return newTree, nil
}

func (r *Repo) GetTreeVersion() uint {
	r.logger.Debug("GetTreeVersion:call")
	defer r.logger.Debug("GetTreeVersion:return")
	r.mux.Lock()
	defer r.mux.Unlock()
	return r.treeVersion
}

func (r *Repo) Name(uuid string) (string, error) {
	r.logger.Debug("Name:call")
	defer r.logger.Debug("Name:return")
	r.mux.Lock()
	defer r.mux.Unlock()
	inst, ok := r.it[uuid]
	if !ok {
		return "", fmt.Errorf("Unknown instance %s", uuid)
	}
	return inst.Name, nil
}

func (r *Repo) GetMySQLInstances() []proto.Instance {
	r.logger.Debug("GetMySQLInstances:call")
	defer r.logger.Debug("GetMySQLInstances:return")
	return onlyMySQLInsts(r.List())
}
