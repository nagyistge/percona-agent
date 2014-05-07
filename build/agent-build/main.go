package main

import (
	"encoding/json"
	"fmt"
	"os"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strings"
)

func runCmd(name string, arg ...string) {
		cmd := exec.Command(name, arg...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		fmt.Printf("Running command: %s\n", name + " " + strings.Join(arg, " "))
		err := cmd.Run()
		if err != nil {
			fmt.Printf("cmd.Run error: %s\n", err)
			os.Exit(1)
		}
}

func chDir(dir string) {
	fmt.Printf("Changing dir to: %s\n", dir)
	err := os.Chdir(dir)
	if err != nil {
		fmt.Printf("os.Chdir error: %s\n", err)
		os.Exit(1)
	}
	pwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("os.Getwd error: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("Currend dir: %s\n", pwd)
}

func main() {
	jsonContent, err := ioutil.ReadFile("../Godeps.json")
	if err != nil {
		fmt.Printf("ioutil.ReadFile error: %s\n", err)
		os.Exit(1)
	}
	type jsonStruct struct {
		ImportPath string
		GoVersion string
		Deps []map[string]string
	}
	var parsedJson jsonStruct
	err = json.Unmarshal(jsonContent, &parsedJson)
	if err != nil {
		fmt.Printf("json.Unmarshal error: %s\n", err)
		os.Exit(1)
	}
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("os.Getwd error: %s\n", err)
		os.Exit(1)
	}
	path := "./vendor"
	gopath := regexp.MustCompile("^" + currentDir + "/" + path)
	if gopath.FindStringIndex(os.Getenv("GOPATH")) == nil {
		if err = os.Setenv("GOPATH", currentDir + "/" + path + string(os.PathListSeparator) + os.Getenv("GOPATH")); err != nil {
			fmt.Printf("os.Setenv error: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("GOPATH set to: %s\n", os.Getenv("GOPATH"))
	}
	site := regexp.MustCompile("^[^/]+")
	dir := regexp.MustCompile("^.+?/[^/]+")
	uri := regexp.MustCompile("^.+?/[^/]+/[^/]+")
	DEPSLOOP:
	for _, value := range parsedJson.Deps {
		url := uri.FindStringSubmatch(value["ImportPath"])
		if _, err = os.Stat(currentDir + "/" + path + "/src/" + url[0]); err != nil {
			if !os.IsNotExist(err) {
				// permission error?
				fmt.Printf("os.Stat error: %s\n", err)
				os.Exit(1)
			}
		} else {
			// folder exists
			continue DEPSLOOP
		}
		folder := dir.FindStringSubmatch(value["ImportPath"])
		domain := site.FindStringSubmatch(value["ImportPath"])
		runCmd("mkdir", "-p", currentDir + "/" + path + "/src/" + folder[0])
		switch domain[0] {
			case "code.google.com":
				runCmd("hg", "clone", "-r", value["Rev"], "https://" + url[0], path + "/src/" + url[0])
			case "github.com":
				chDir(currentDir + "/" + path + "/src/" + folder[0])
				runCmd("git", "clone", "https://" + url[0])
				chDir(currentDir + "/" + path + "/src/" + url[0])
				runCmd("git", "checkout", value["Rev"])
		}
	}
	chDir(currentDir + "/../bin/percona-agent/")
	runCmd("go", "build", "percona-agent.go")
}
