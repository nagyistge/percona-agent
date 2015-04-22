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

package system

import (
	"fmt"
	"github.com/percona/cloud-protocol/proto/v2"
	"github.com/percona/percona-agent/pct"
	"github.com/percona/percona-agent/pct/cmd"
)

const (
	TOOL_NAME     = "system"
	PT_SLEEP_SECONDS = "4"
)

type System struct {
	CmdName string
	logger  *pct.Logger
}

func NewSystem(logger *pct.Logger) *System {
	return &System{
		CmdName: "pt-summary",
		logger:  logger,
	}
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (s *System) Handle(protoCmd *proto.Cmd) *proto.Reply {
	args := []string{
		"--sleep", PT_SLEEP_SECONDS,
	}
	ptSummary := cmd.NewRealCmd(s.CmdName, args...)
	output, err := ptSummary.Run()
	if err != nil {
		s.logger.Error(fmt.Sprintf("%s: %s", s.CmdName, err))
	}

	result := &proto.SysinfoResult{
		Raw: output,
	}

	return protoCmd.Reply(result, err)
}
