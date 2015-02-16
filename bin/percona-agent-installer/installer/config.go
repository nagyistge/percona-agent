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
	pctLog "github.com/percona/percona-agent/log"
	"github.com/percona/percona-agent/pct"
)

func (i *Installer) writeInstances(si *proto.ServerInstance, mi *proto.MySQLInstance) error {
	// We could write the instance structs directly, but this is the job of an
	// instance repo and it's easy enough to create one, so do the right thing.
	if si != nil {
		bytes, err := json.Marshal(si)
		if err != nil {
			return err
		}
		if err := i.instanceRepo.Add("server", si.Id, bytes, true); err != nil {
			return err
		}
	}
	if mi != nil {
		bytes, err := json.Marshal(mi)
		if err != nil {
			return err
		}
		if err := i.instanceRepo.Add("mysql", mi.Id, bytes, true); err != nil {
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
