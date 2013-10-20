package test

import (
	"os"
	"fmt"
	"io/ioutil"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/qa"
	"encoding/json"
)

func RunQhWorker(job *qa.Job) string {
	cc := &agent.ControlChannels{
		LogChan: make(chan *log.LogEntry),
		StopChan: make(chan bool),
	}
	resultChan := make(chan *qa.Result, 1)
	doneChan := make(chan *qa.Worker, 1)

	w := qa.NewWorker(cc, job, resultChan, doneChan)
	w.Run()

	// Write the result as formatted JSON to a file...
	result := <-resultChan
	tmpFilename := fmt.Sprintf("/tmp/pct-test.%d", os.Getpid())
	WriteData(result, tmpFilename)
	return tmpFilename
}

func WriteData(data interface{}, filename string) {
	bytes, _ := json.MarshalIndent(data, "", " ")
	bytes = append(bytes, 0x0A) // newline
	ioutil.WriteFile(filename, bytes, os.ModePerm)
}
