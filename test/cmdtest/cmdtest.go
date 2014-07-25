package cmdtest

import (
	"bytes"
	"io"
	"log"
	"os/exec"
	"time"
)

type CmdTest struct {
	reader io.Reader
	stdin  io.Writer
	output <-chan string
}

func NewCmdTest(cmd *exec.Cmd) *CmdTest {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	cmdOutput := &CmdTest{
		reader: pipeReader,
		stdin:  stdin,
	}
	cmdOutput.output = cmdOutput.Run()
	return cmdOutput
}

func (c *CmdTest) Run() <-chan string {
	output := make(chan string, 1024)
	go func() {
		for {
			b := make([]byte, 8192)
			n, err := c.reader.Read(b)
			if n > 0 {
				lines := bytes.SplitAfter(b[:n], []byte("\n"))
				// Example: Split(a\nb\n\c\n) => ["a\n", "b\n", "c\n", ""]
				// We are getting empty element because data for split was ending with delimeter (\n)
				// We don't want it, so we remove it
				lastPos := len(lines) - 1
				if len(lines[lastPos]) == 0 {
					lines = lines[:lastPos]
				}
				for i := range lines {
					line := string(lines[i])
					log.Printf("%#v", line)
					output <- line
				}
			}
			if err != nil {
				break
			}
		}
	}()
	return output
}

func (c *CmdTest) ReadLine() (line string) {
	select {
	case line = <-c.output:
	case <-time.After(400 * time.Millisecond):
	}
	return line
}

func (c *CmdTest) Write(data string) {
	_, err := c.stdin.Write([]byte(data))
	if err != nil {
		panic(err)
	}
}
