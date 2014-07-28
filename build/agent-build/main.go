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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	rootDir    string
	currentDir string
)

type Dep struct {
	Pkg string
	Rev string
}

type Godeps struct {
	GoVersion string
	Deps      []Dep
}

var (
	flagDeps  bool
	flagBuild bool
)

func init() {
	flag.BoolVar(&flagDeps, "deps", true, "Process Godeps.json")
	flag.BoolVar(&flagBuild, "build", true, "Build percona-agent")

	log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)

	_, filename, _, _ := runtime.Caller(1)
	dir := filepath.Dir(filename)

	for i := 0; i < 3; i++ {
		dir = dir + "/../"
		if FileExists(dir+"COPYING") && FileExists(dir+".git") {
			rootDir = filepath.Clean(dir)
			break
		}
	}
	if rootDir == "" {
		log.Panic("Cannot find repo root dir")
	}
	log.Printf("Root dir: %s\n", rootDir)

	currentDir, _ = os.Getwd()
}

func runCmd(name string, arg ...string) error {
	fmt.Printf("%s\n", name+" "+strings.Join(arg, " "))
	out, err := exec.Command(name, arg...).CombinedOutput()
	if err != nil {
		log.Println(string(out))
	}
	return err
}

func chDir(dir string) {
	err := os.Chdir(dir)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()

	// Parse Godeps.json file.
	godepsContent, err := ioutil.ReadFile(rootDir + "/Godeps.json")
	if err != nil {
		log.Fatalf("ioutil.ReadFile error: %s\n", err)
	}
	godeps := &Godeps{}
	err = json.Unmarshal(godepsContent, godeps)
	if err != nil {
		log.Fatalf("json.Unmarshal error: %s\n", err)
	}

	// Prefix GOPATH with vendor dir.
	vendorDir := filepath.Join(rootDir, "vendor")
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		log.Fatal("GOPATH not set")
	}
	if err := os.Setenv("GOPATH", vendorDir+string(os.PathListSeparator)+gopath); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("GOPATH: %s\n", os.Getenv("GOPATH"))

	// Checkout the required revision of dep.
	if flagDeps {
		for _, dep := range godeps.Deps {
			fmt.Printf("%s %s\n", dep.Pkg, dep.Rev)
			pkgDir := filepath.Join(vendorDir, "src", dep.Pkg)
			if !FileExists(pkgDir) {
				if err := runCmd("mkdir", "-p", pkgDir); err != nil {
					log.Fatal(err)
				}
			}
			p := strings.Split(dep.Pkg, "/")
			switch p[0] {
			case "code.google.com":
				chDir(rootDir)
				if err := os.RemoveAll(pkgDir); err != nil {
					log.Fatal(err)
				}
				if err := runCmd("hg", "clone", "-r", dep.Rev, "https://"+dep.Pkg, pkgDir); err != nil {
					log.Fatal(err)
				}
			case "github.com", "gopkg.in":
				chDir(pkgDir)
				if !FileExists(".git") {
					if err := runCmd("git", "clone", "https://"+dep.Pkg, "."); err != nil {
						log.Fatal(err)
					}
				} else {
					if err := runCmd("git", "checkout", "master"); err != nil {
						log.Fatal(err)
					}
					if err := runCmd("git", "pull"); err != nil {
						log.Fatal(err)
					}
				}
				if err := runCmd("git", "checkout", dep.Rev); err != nil {
					log.Fatal(err)
				}
			}
		}
	}

	// Build percona-agent.
	if flagBuild {
		log.Println("Building percona-agent...")
		chDir(rootDir + "/bin/percona-agent")
		if err := runCmd("go", "build"); err != nil {
			log.Fatal(err)
		}
	}

	chDir(currentDir)
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
