package main

import (
	"flag"
	"fmt"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/pct"
	"log"
	"os"
)

var (
	flagApiHostname string
	flagApiKey      string
	flagDebug       bool
	flagSkipMySQL   bool
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	flag.StringVar(&flagApiHostname, "api-host", "", "API host")
	flag.StringVar(&flagApiKey, "api-key", "", "API key")
	flag.BoolVar(&flagDebug, "debug", false, "Debug")
	flag.BoolVar(&flagSkipMySQL, "skip-mysql", false, "Skip MySQL steps")
	flag.Parse()
}

func main() {
	agentConfig := &agent.Config{
		ApiHostname: flagApiHostname,
		ApiKey:      flagApiKey,
	}
	flags := Flags{
		"debug":      flagDebug,
		"skip-mysql": flagSkipMySQL,
	}
	installer := NewInstaller(NewTerminal(os.Stdin, flags), pct.NewAPI(), agentConfig, flags)
	fmt.Println("CTRL-C at any time to quit")
	// todo: catch SIGINT and clean up
	if err := installer.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}
