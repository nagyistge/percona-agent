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

package main

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/percona-agent/mm"
	"github.com/percona/percona-agent/qan"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dataDir := ParseCmdLine()
	fmt.Println(dataDir)

	dataFiles, _ := filepath.Glob(dataDir + "/*")
	for _, file := range dataFiles {
		fmt.Println(file)

		content, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Println(err)
			continue
		}
		data := &proto.Data{}
		if err := json.Unmarshal(content, data); err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Printf("Ts: %s Hostname: %s Service: %s\n", data.Created, data.Hostname, data.Service)

		if strings.Contains(file, "mm_") {
			report := &mm.Report{}
			if err := json.Unmarshal(data.Data, report); err != nil {
				fmt.Println(err)
				continue
			}
			bytes, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(bytes))
		} else if strings.Contains(file, "qan_") {
			report := &qan.Report{}
			if err := json.Unmarshal(data.Data, report); err != nil {
				fmt.Println(err)
				continue
			}
			bytes, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(bytes))
		}
	}
}

func ParseCmdLine() string {
	usage := "Usage: percona-agent-data <data dir>"
	if len(os.Args) != 2 {
		fmt.Println(usage)
		os.Exit(-1)
	}
	return os.Args[1]
}
