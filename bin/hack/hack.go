package main

import (
	"encoding/json"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/client"
	"github.com/percona/cloud-tools/log"
	"github.com/percona/cloud-tools/pct"
	"io/ioutil"
	golog "log"
	"net/http"
	"os"
	"os/user"
	"time"
)

func init() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
}

func main() {

	Get(
		"00000000000000000000000000000001",
		"http://localhost:8000/agents/00000000-0000-0000-0000-000000000001/status",
	)
	return

	var links map[string]string
	var err error
	if links, err = GetLinks("00000000000000000000000000000001", "http://localhost:8000/agents/00000000-0000-0000-0000-000000000001"); err != nil {
		golog.Fatalln(err)
	}
	golog.Printf("links: %+v\n", links)

	logChan := make(chan *proto.LogEntry, log.BUFFER_SIZE*3)

	// Log websocket client, possibly disabled later.
	logClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "log-ws"), links["log"], "http://localhost", "00000000000000000000000000000001")
	if err != nil {
		golog.Fatalln(err)
	}
	logManager := log.NewManager(logClient, logChan)
	logConfig := log.Config{
		Level: "debug",
		File:  "STDOUT",
	}
	logConfigData, _ := json.Marshal(logConfig)
	if err := logManager.Start(&proto.Cmd{}, logConfigData); err != nil {
		golog.Panicf("Error starting log service: ", err)
	}

	logger := pct.NewLogger(logChan, "hack")
	logger.Info("test")

	time.Sleep(10 * time.Second)
}

func GetLinks(apiKey, url string) (map[string]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		golog.Fatal(err)
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	golog.Println("Getting entry links from", url)

	backoff := pct.NewBackoff(5 * time.Minute)
	for {
		time.Sleep(backoff.Wait())

		resp, err := client.Do(req)
		if err != nil {
			golog.Printf("GET %s error: client.Do: %s", url, err)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			golog.Printf("GET %s error: ioutil.ReadAll: %s", url, err)
			continue
		}

		if resp.StatusCode >= 400 {
			golog.Printf("Error %d from %s\n", resp.StatusCode, url)
		} else if len(body) == 0 {
			golog.Println("OK response from ", url, "but no content")
		}

		links := &proto.Links{}
		if err := json.Unmarshal(body, links); err != nil {
			golog.Printf("GET %s error: json.Unmarshal: %s: %s", url, err, string(body))
			continue
		}

		return links.Links, nil
	}
}

func MakeAgentAuth(config *agent.Config) (*proto.AgentAuth, string) {
	hostname, _ := os.Hostname()
	u, _ := user.Current()
	username := u.Username
	origin := "http://" + username + "@" + hostname

	auth := &proto.AgentAuth{
		ApiKey:   config.ApiKey,
		Uuid:     config.AgentUuid,
		Hostname: hostname,
		Username: username,
	}
	return auth, origin
}

func Get(apiKey, url string) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		golog.Fatal(err)
	}
	req.Header.Add("X-Percona-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		golog.Printf("GET %s error: client.Do: %s", url, err)
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		golog.Printf("GET %s error: ioutil.ReadAll: %s", url, err)
		return
	}

	golog.Printf("%+v\n", string(body))
}
