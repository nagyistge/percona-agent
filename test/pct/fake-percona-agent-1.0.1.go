package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}
	if os.Args[1] != "-version" {
		os.Exit(1)
	}
	fmt.Println("percona-agent 1.0.1")
}
