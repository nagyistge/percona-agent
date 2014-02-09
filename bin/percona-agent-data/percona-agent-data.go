package main

import (
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/mm"
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
			fmt.Printf("%#v\n", report)

			metrics := report.Metrics
			for metric, stats := range metrics {
				fmt.Printf("  %s: %+v\n", metric, *stats)
			}
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
