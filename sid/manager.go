package sid

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

const (
	PREFIX = "instance-"
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
	files, err := filepath.Glob(m.configDir + "/" + PREFIX + "*")
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
				return errors.New("sid:Init:json.Unmarshal:" + file + ":" + err.Error())
			}
			info = it
		case "mysql":
			it := &proto.MySQLInstance{}
			if err := json.Unmarshal(data, it); err != nil {
				return errors.New("sid:Init:json.Unmarshal:" + file + ":" + err.Error())
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
	name := name(service, id)
	if _, ok := m.it[name]; ok {
		return pct.DuplicateServiceInstanceError{Service: service, Id: id}
	}

	file := m.configDir + "/" + PREFIX + name
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
	name := name(service, id)

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
	infoVal.Set(reflect.ValueOf(it).Elem())

	return nil
}

func (m *Manager) Update(service string, id uint, info []byte) error {
	// todo: API --> agent --> sid.Update()
	// After successful update, agent should restart all services using
	// the instance and reply ok only if all service restart ok.
	return nil
}

func (m *Manager) Remove(sid uint) error {
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

func name(service string, id uint) string {
	return fmt.Sprintf("%s-%d", service, id)
}
