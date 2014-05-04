package main

import (
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-protocol/proto"
	"github.com/percona/cloud-tools/agent"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"log"
	"math/rand"
	"net/http"
)

func (i *Installer) createMySQLUser(dsn mysql.DSN) (mysql.DSN, error) {
	// Same host:port or socket, but different user and pass.
	userDSN := dsn
	userDSN.Username = "percona-agent"
	userDSN.Password = fmt.Sprintf("%p%d", &dsn, rand.Uint32())

	dsnString, _ := dsn.DSN()
	conn := mysql.NewConnection(dsnString)
	if err := conn.Connect(1); err != nil {
		return userDSN, err
	}
	defer conn.Close()

	grant := MakeGrant(dsn)
	if i.flags["debug"] {
		log.Println(grant)
	}
	sql := fmt.Sprintf(grant, userDSN.Username, userDSN.Password)
	_, err := conn.DB().Exec(sql)
	return userDSN, err
}

func (i *Installer) createServerInstance() (*proto.ServerInstance, error) {
	// POST <api>/instances/server
	si := &proto.ServerInstance{
		Hostname: i.hostname,
	}
	data, err := json.Marshal(si)
	if err != nil {
		return nil, err
	}
	url := pct.URL(i.agentConfig.ApiHostname, "instances", "server")
	if i.flags["debug"] {
		log.Println(url)
	}
	resp, _, err := i.api.Post(i.agentConfig.ApiKey, url, data)
	if i.flags["debug"] {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	// Create new instance, if it already exist then just use it
	// todo: better handling of duplicate instance
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("Failed to create server instance (status code %d)", resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, fmt.Errorf("API did not return location of new server instance")
	}

	// GET <api>/instances/server/id (URI)
	code, data, err := i.api.Get(i.agentConfig.ApiKey, uri)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get new server instance (status code %d)", code)
	}
	if err := json.Unmarshal(data, si); err != nil {
		return nil, err
	}
	return si, nil
}

func (i *Installer) createMySQLInstance(dsn mysql.DSN) (*proto.MySQLInstance, error) {
	// First use instance.Manager to fill in details about the MySQL server.
	dsnString, _ := dsn.DSN()
	mi := &proto.MySQLInstance{
		Hostname: i.hostname,
		DSN:      dsnString,
	}
	if err := instance.GetMySQLInfo(mi); err != nil {
		if i.flags["debug"] {
			log.Printf("err=%s\n", err)
		}
		return nil, err
	}

	// POST <api>/instances/mysql
	data, err := json.Marshal(mi)
	if err != nil {
		return nil, err
	}
	url := pct.URL(i.agentConfig.ApiHostname, "instances", "mysql")
	if i.flags["debug"] {
		log.Println(url)
	}
	resp, _, err := i.api.Post(i.agentConfig.ApiKey, url, data)
	if i.flags["debug"] {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	// Create new instance, if it already exist then just use it
	// todo: better handling of duplicate instance
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("Failed to create MySQL instance (status code %d)", resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, fmt.Errorf("API did not return location of new MySQL instance")
	}

	// GET <api>/instances/mysql/id (URI)
	code, data, err := i.api.Get(i.agentConfig.ApiKey, uri)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get new MySQL instance (status code %d)", code)
	}
	if err := json.Unmarshal(data, mi); err != nil {
		return nil, err
	}
	return mi, nil
}

func (i *Installer) createAgent(configs []proto.AgentConfig) (*proto.Agent, error) {
	agent := &proto.Agent{
		Hostname: i.hostname,
		Version:  agent.VERSION,
		Configs:  configs,
	}
	data, err := json.Marshal(agent)
	if err != nil {
		return nil, err
	}
	url := pct.URL(i.agentConfig.ApiHostname, "agents")
	if i.flags["debug"] {
		log.Println(url)
	}
	resp, _, err := i.api.Post(i.agentConfig.ApiKey, url, data)
	if i.flags["debug"] {
		log.Printf("resp=%#v\n", resp)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("Failed to create server instance (status code %d)", resp.StatusCode)
	}

	// API returns URI of new resource in Location header
	uri := resp.Header.Get("Location")
	if uri == "" {
		return nil, fmt.Errorf("API did not return location of new agent")
	}

	// GET <api>/agents/:uuid
	code, data, err := i.api.Get(i.agentConfig.ApiKey, uri)
	if i.flags["debug"] {
		log.Printf("code=%d\n", code)
		log.Printf("err=%s\n", err)
	}
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("Failed to get new agent (status code %d)", code)
	}
	if err := json.Unmarshal(data, agent); err != nil {
		return nil, err
	}
	return agent, nil
}
