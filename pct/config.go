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

package pct

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

func ReadConfig(file string, v interface{}) error {
	data, err := ioutil.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		// There's an error and it's not "file not found".
		return err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &v)
	}
	return err
}

func WriteConfig(file string, config interface{}) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(file, data, 0644)
	return err
}
