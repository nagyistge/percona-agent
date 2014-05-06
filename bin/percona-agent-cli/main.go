package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/percona/cloud-protocol/proto"
	"io/ioutil"
	golog "log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	VERSION = "1.0.0"
)

func init() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
}

type Cli struct {
	// State
	apiHostname string
	apiKey      string
	connected   bool
	agentUuid   string
	client      *http.Client
	entryLinks  map[string]string
	agentLinks  map[string]string
}

func main() {
	cli := &Cli{}
	cli.Run()
}

func (cli *Cli) Run() {
	fmt.Printf("percona-agent-cli %s\nType '?' for help.\nUse 'connect' to get started.\n\n", VERSION)

	bio := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s@%s> ", cli.agentUuid, cli.apiHostname)
		line, _, err := bio.ReadLine()
		if err != nil {
			golog.Println(err)
			continue
		}
		lines := strings.Split(string(line), ";")
		for _, line := range lines {
			args := strings.Split(strings.TrimSpace(line), " ")
			if len(args) == 0 {
				continue
			}
			cli.doCmd(args)
		}
	}
}

func (cli *Cli) doCmd(args []string) {
	t0 := time.Now()

	switch args[0] {
	case "":
		return
	case "?", "help":
		cli.help()
		return
	case "connect":
		cli.connect(args)
	case "agent":
		cli.agent(args)
	case "status":
		cli.status(args)
	case "config":
		cli.config(args)
	case "send":
		cli.send(args)
	default:
		fmt.Println("Unknown command: " + args[0])
		return
	}

	d := time.Now().Sub(t0)
	fmt.Printf("%s %s\n", args[0], d)
}

func (cli *Cli) help() {
	fmt.Printf("Commands:\n  connect\n  agent\n  status\n  ?\n\n")
	fmt.Printf("Prompt:\n  agent@api>\n  Use 'connect' command to connect to API, then 'agent' command to set agent.\n\n")
	fmt.Printf("CTRL-C to exit\n\n")
}

func (cli *Cli) connect(args []string) {
	if cli.client == nil {
		cli.client = &http.Client{}
	}

	if len(args) != 3 {
		fmt.Printf("ERROR: Invalid number of args: got %d, expected 3\n", len(args))
		fmt.Println("Usage: connect api-hostname api-key")
		fmt.Println("Exmaple: connect http://localhost:8000 00000000000000000000000000000001")
		return
	}

	apiHostname := args[1]
	cli.apiKey = args[2] // set now because Get() uses it

	data := cli.Get(apiHostname)
	links := &proto.Links{}
	if err := json.Unmarshal(data, links); err != nil {
		golog.Printf("GET %s error: json.Unmarshal: %s: %s", apiHostname, err, string(data))
		return
	}

	if _, ok := links.Links["agents"]; !ok {
		fmt.Println("ERROR: Connected but no agents link.  Try to connect again.  API has bug if problem continues.")
		return
	}

	cli.apiHostname = apiHostname
	cli.entryLinks = links.Links
	cli.connected = true

	fmt.Printf("Entry links:\n%+v\n\n", cli.entryLinks)
}

func (cli *Cli) agent(args []string) {
	if !cli.connected {
		fmt.Println("Not connected to API.  Use 'connect' command.")
		return
	}

	if len(args) == 1 && cli.agentUuid != "" {
		cli.agentUuid = ""
		cli.agentLinks = make(map[string]string)
		return
	}

	if len(args) != 2 {
		fmt.Printf("ERROR: Invalid number of args: got %d, expected 2\n", len(args))
		fmt.Println("Usage: agent agent-uuid")
		fmt.Println("Exmaple: agent 00000000-0000-0000-0000-000000000001")
		return
	}

	uuid := args[1]

	url := cli.entryLinks["agents"] + "/" + uuid
	data := cli.Get(url)
	links := &proto.Links{}
	if err := json.Unmarshal(data, links); err != nil {
		golog.Printf("GET %s error: json.Unmarshal: %s: %s", url, err, string(data))
		return
	}

	needLinks := []string{"self", "cmd", "log", "data"}
	for _, needLink := range needLinks {
		if _, ok := links.Links[needLink]; !ok {
			fmt.Println("ERROR: API did not return a %s link.  Reconnect and try again.\n", needLink)
			return
		}
	}

	cli.agentUuid = uuid
	cli.agentLinks = links.Links
	fmt.Printf("Agent links:\n%+v\n\n", cli.agentLinks)
}

func (cli *Cli) status(args []string) {
	if !cli.connected {
		fmt.Println("Not connected to API.  Use 'connect' command.")
		return
	}
	if cli.agentUuid == "" {
		fmt.Println("Agent UUID not set.  Use 'agent' command.")
		return
	}
	status := cli.Get(cli.agentLinks["self"] + "/status")
	fmt.Println(string(status))
}

func (cli *Cli) config(args []string) {
	if !cli.connected {
		fmt.Println("Not connected to API.  Use 'connect' command.")
		return
	}
	if cli.agentUuid == "" {
		fmt.Println("Agent UUID not set.  Use 'agent' command.")
		return
	}

	if len(args) != 3 {
		fmt.Printf("ERROR: Invalid number of args: got %d, expected 3\n", len(args))
		fmt.Println("Usage: config update file")
		fmt.Println("Exmaple: config update /tmp/new-log.conf")
		return
	}

	if args[1] != "update" {
		fmt.Printf("Invalid arg: got %s, expected 'config'n", args[1])
		return
	}

	// todo
	_, err := ioutil.ReadFile(args[2])
	if err != nil {
		golog.Println(err)
		return
	}
}

func (cli *Cli) send(args []string) {
	if !cli.connected {
		fmt.Println("Not connected to API.  Use 'connect' command.")
		return
	}
	if cli.agentUuid == "" {
		fmt.Println("Agent UUID not set.  Use 'agent' command.")
		return
	}
	if len(args) < 3 {
		fmt.Printf("ERROR: Invalid number of args: got %d, expected 3\n", len(args))
		fmt.Println("Usage: send cmd service")
		fmt.Println("Exmaple: send Stop agent")
		return
	}
	cmd := &proto.Cmd{
		Ts:        time.Now(),
		User:      "percona-agent-cli",
		AgentUuid: cli.agentUuid,
		Cmd:       args[1],
		Service:   args[2],
	}
	if len(args) == 4 {
		switch args[1] {
		case "Update":
			cmd.Data = []byte(args[3])
		}
	}
	reply, err := cli.Put(cli.agentLinks["self"]+"/cmd", cmd)
	if err != nil {
		golog.Println(err)
		return
	}
	if reply.Error != "" {
		fmt.Printf("ERROR: %s\n", reply.Error)
		return
	}
	fmt.Println("OK")
	switch cmd.Cmd {
	case "Version":
		v := &proto.Version{}
		if err := json.Unmarshal(reply.Data, v); err != nil {
			fmt.Printf("Invalid Version reply: %s\n", err)
			return
		}
		fmt.Printf("%#v\n", v)
	}
}

func (cli *Cli) Get(url string) []byte {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		golog.Fatal(err)
	}
	req.Header.Add("X-Percona-API-Key", cli.apiKey)

	resp, err := cli.client.Do(req)
	if err != nil {
		golog.Printf("GET %s error: client.Do: %s", url, err)
		return nil
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		golog.Printf("GET %s error: ioutil.ReadAll: %s", url, err)
		return nil
	}
	return body
}

func (cli *Cli) Put(url string, cmd *proto.Cmd) (*proto.Reply, error) {
	golog.Printf("POST %s\n", url)
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(data)
	req, err := http.NewRequest("PUT", url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Percona-API-Key", cli.apiKey)
	resp, err := cli.client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	reply := &proto.Reply{}
	if err := json.Unmarshal(body, reply); err != nil {
		return nil, err
	}
	return reply, nil
}
