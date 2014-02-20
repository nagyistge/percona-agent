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

package mm

const CONFIG_FILE = "mm.conf"

type Interval struct {
	Collect uint // how often monitor collects metrics (seconds)
	Report  uint // how often aggregator reports metrics (seconds)
}

type Config struct {
	Intervals map[string]Interval // e.g. mysql=>{Collect:1, Report:60}
}
