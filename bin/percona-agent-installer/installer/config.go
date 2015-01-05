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

package installer

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/data"
	"github.com/percona/percona-agent/instance"
	pctLog "github.com/percona/percona-agent/log"
	mmMySQL "github.com/percona/percona-agent/mm/mysql"
	mmServer "github.com/percona/percona-agent/mm/system"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/qan"
	sysconfigMySQL "github.com/percona/percona-agent/sysconfig/mysql"
	"log"
	"net/http"
)

func (i *Installer) getMmServerConfig(si *proto.ServerInstance) (*proto.AgentConfig, error) {
	url := pct.URL(i.agentConfig.ApiHostname, "/configs/mm/default-server")
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags.Bool["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default server monitor config (%s, status %d)", url, code)
	}
	config := &mmServer.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "server"
	config.InstanceId = si.Id

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		InternalService: "mm",
		ExternalService: proto.ServiceInstance{
			Service:    "server",
			InstanceId: si.Id,
		},
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

func (i *Installer) getMmMySQLConfig(mi *proto.MySQLInstance) (*proto.AgentConfig, error) {
	url := pct.URL(i.agentConfig.ApiHostname, "/configs/mm/default-mysql")
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags.Bool["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL monitor config (%s, status %d)", url, code)
	}
	config := &mmMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = mi.Id

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		InternalService: "mm",
		ExternalService: proto.ServiceInstance{
			Service:    "mysql",
			InstanceId: mi.Id,
		},
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

func (i *Installer) getSysconfigMySQLConfig(mi *proto.MySQLInstance) (*proto.AgentConfig, error) {
	url := pct.URL(i.agentConfig.ApiHostname, "/configs/sysconfig/default-mysql")
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags.Bool["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default MySQL sysconfig config (%s, status %d)", url, code)
	}
	config := &sysconfigMySQL.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = mi.Id

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		InternalService: "sysconfig",
		ExternalService: proto.ServiceInstance{
			Service:    "mysql",
			InstanceId: mi.Id,
		},
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

func (i *Installer) getQanConfig(mi *proto.MySQLInstance) (*proto.AgentConfig, error) {
	url := pct.URL(i.agentConfig.ApiHostname, "/configs/qan/default")
	code, data, err := i.api.Get(i.agentConfig.ApiKey, url)
	if i.flags.Bool["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get default Query Analytics config (%s, status %d)", url, code)
	}
	config := &qan.Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	config.Service = "mysql"
	config.InstanceId = mi.Id

	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		InternalService: "qan",
		ExternalService: proto.ServiceInstance{
			Service:    "mysql",
			InstanceId: mi.Id,
		},
		Config:  string(bytes),
		Running: true,
	}
	return agentConfig, nil
}

func (i *Installer) writeInstances(si *proto.ServerInstance, mi *proto.MySQLInstance) error {
	// We could write the instance structs directly, but this is the job of an
	// instance repo and it's easy enough to create one, so do the right thing.
	logChan := make(chan *proto.LogEntry, 100)
	logger := pct.NewLogger(logChan, "instance-repo")
	repo := instance.NewRepo(logger, pct.Basedir.Dir("config"), i.api)
	if si != nil {
		bytes, err := json.Marshal(si)
		if err != nil {
			return err
		}
		if err := repo.Add("server", si.Id, bytes, true); err != nil {
			return err
		}
	}
	if mi != nil {
		bytes, err := json.Marshal(mi)
		if err != nil {
			return err
		}
		if err := repo.Add("mysql", mi.Id, bytes, true); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) writeConfigs(agentRes *proto.Agent, configs []proto.AgentConfig) error {
	// A little confusing but agent.Config != proto.Agent != proto.AgentConfig.
	// agent.Config is what we need because this is the main config file.
	i.agentConfig.AgentUuid = agentRes.Uuid
	i.agentConfig.Links = agentRes.Links
	if err := pct.Basedir.WriteConfig("agent", i.agentConfig); err != nil {
		return err
	}

	logConfig := pctLog.Config{
		File:  pctLog.DEFAULT_LOG_FILE,
		Level: pctLog.DEFAULT_LOG_LEVEL,
	}
	if err := pct.Basedir.WriteConfig("log", logConfig); err != nil {
		return err
	}

	dataConfig := data.Config{
		Encoding:     data.DEFAULT_DATA_ENCODING,
		SendInterval: data.DEFAULT_DATA_SEND_INTERVAL,
	}
	if err := pct.Basedir.WriteConfig("data", dataConfig); err != nil {
		return err
	}

	for _, config := range configs {
		name := config.InternalService
		if name != "qan" {
			name += fmt.Sprintf("-%s-%d", config.ExternalService.Service, config.ExternalService.InstanceId)
		}
		if err := pct.Basedir.WriteConfigString(name, config.Config); err != nil {
			return err
		}
	}

	return nil
}
