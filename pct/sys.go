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
	"os"
	"path/filepath"
)

func FileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

func SameFile(file1, file2 string) (bool, error) {
	var err error

	stat1, err := os.Stat(file1)
	if err != nil {
		return false, err
	}

	stat2, err := os.Stat(file2)
	if err != nil {
		return false, err
	}

	return os.SameFile(stat1, stat2), nil
}

func MakeDir(file string) error {
	dir := filepath.Dir(file)
	if err := os.Mkdir(dir, 0755); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func RemoveFile(file string) error {
	if file != "" {
		err := os.Remove(file)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func FileExists(file string) bool {
	_, err := os.Stat(file)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}
