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
	"os"
)

var ConfigDir string

func WriteConfig(service string, config interface{}) error {
	if ConfigDir == "" {
		return nil // shouldn't happen
	}

	configJson, err := json.Marshal(config)
	if err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	file, err := os.OpenFile(ConfigDir+"/"+service+".conf", flags, 0644)
	if err != nil {
		return err
	}

	if _, err = file.WriteString(string(configJson)); err != nil {
		return err
	}

	return nil
}
