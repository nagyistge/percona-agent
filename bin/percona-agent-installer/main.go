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
	flagBasedir     string
	flagDebug       bool
	flagSkipMySQL   bool
	flagSkipAgent   bool
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	flag.StringVar(&flagApiHostname, "api-host", agent.DEFAULT_API_HOSTNAME, "API host")
	flag.StringVar(&flagApiKey, "api-key", "", "API key")
	flag.StringVar(&flagBasedir, "basedir", pct.DEFAULT_BASEDIR, "Agent basedir")
	flag.BoolVar(&flagDebug, "debug", false, "Debug")
	flag.BoolVar(&flagSkipMySQL, "skip-mysql", false, "Skip MySQL steps")
	flag.BoolVar(&flagSkipAgent, "skip-agent", false, "Skip agent steps")
	flag.Parse()
}

func main() {
	agentConfig := &agent.Config{
		ApiHostname: flagApiHostname,
		ApiKey:      flagApiKey,
	}
	// todo: do flags a better way
	flags := Flags{
		"debug":      flagDebug,
		"skip-mysql": flagSkipMySQL,
		"skip-agent": flagSkipAgent,
	}

	// Agent stores all its files in the basedir.  This must be called first
	// because installer uses pct.Basedir and assumes it's already initialized.
	if err := pct.Basedir.Init(flagBasedir); err != nil {
		log.Printf("Error initializing basedir %s: %s\n", flagBasedir, err)
		os.Exit(1)
	}

	installer := NewInstaller(NewTerminal(os.Stdin, flags), flagBasedir, pct.NewAPI(), agentConfig, flags)
	fmt.Println("CTRL-C at any time to quit")
	// todo: catch SIGINT and clean up
	if err := installer.Run(); err != nil {
		fmt.Println(err)
		fmt.Println("Install failed")
		os.Exit(1)
	}
	fmt.Println("Install successful")
	os.Exit(0)
}
