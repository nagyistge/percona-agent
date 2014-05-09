package main

import (
	"encoding/json"
	"fmt"
	"github.com/percona/percona-agent/agent"
	"github.com/percona/percona-agent/mysql"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

type PTAgentConfig struct {
	Uuid string `json:"uuid"`
}

func GetPTAgentSettings(ptagentConf string) (*agent.Config, *mysql.DSN, error) {
	agent := &agent.Config{}
	dsn := &mysql.DSN{Hostname: "localhost"}

	/**
	 * Parse ~/.pt-agent.conf to get API key, MySQL socket, and agent's lib dir.
	 */

	content, err := ioutil.ReadFile(ptagentConf)
	if err != nil {
		return nil, nil, err
	}
	libDir, err := ParsePTAgentConf(string(content), agent, dsn)
	if err != nil {
		return nil, nil, err
	}
	if libDir == "" {
		libDir = "/var/lib/pt-agent"
	}
	fmt.Printf("pt-agent lib dir: %s\n", libDir)

	/**
	 * Parse <libDir>/agent to get agent's UUID.
	 */

	content, err = ioutil.ReadFile(filepath.Join(libDir, "agent"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if err := ParsePTAgentResource(content, agent); err != nil {
		return nil, nil, err
	}

	/**
	 * Parse /etc/percona/agent/my.cnf to get agent's MySQL user and pass.
	 */

	content, err = ioutil.ReadFile("/etc/percona/agent/my.cnf")
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if err := ParseMyCnf(string(content), dsn); err != nil {
		return nil, nil, err
	}

	return agent, dsn, nil
}

func StopPTAgent() error {
	out, err := exec.Command("pt-agent", "--stop").Output()
	if err != nil {
		fmt.Println(string(out))
	}
	return err
}

func ParsePTAgentConf(content string, agent *agent.Config, dsn *mysql.DSN) (string, error) {
	libDir := ""
	if content == "" {
		return libDir, nil
	}

	libDirRe := regexp.MustCompile(`lib\s*=(\S+)`)
	m := libDirRe.FindStringSubmatch(content)
	if len(m) > 1 {
		libDir = m[1]
		fmt.Printf("pt-agent lib dir: %s\n", libDir)
	}

	apiKeyRe := regexp.MustCompile(`^\s*api-key\s*=(\S+)`)
	m = apiKeyRe.FindStringSubmatch(content)
	if len(m) > 1 {
		agent.ApiKey = m[1]
		fmt.Printf("pt-agent API key: %s\n", agent.ApiKey)
	}

	socketRe := regexp.MustCompile(`socket\s*=(\S+)`)
	m = socketRe.FindStringSubmatch(content)
	if len(m) > 1 {
		dsn.Socket = m[1]
		fmt.Printf("pt-agent socket: %s\n", dsn.Socket)
	}

	return libDir, nil
}

func ParsePTAgentResource(content []byte, agent *agent.Config) error {
	if content != nil && len(content) == 0 {
		return nil
	}
	config := &PTAgentConfig{}
	if err := json.Unmarshal(content, config); err != nil {
		return err
	}
	agent.AgentUuid = config.Uuid
	fmt.Printf("pt-agent UUID: %s\n", agent.AgentUuid)
	return nil
}

func ParseMyCnf(content string, dsn *mysql.DSN) error {
	if content == "" {
		return nil
	}
	userRe := regexp.MustCompile(`user\s*=(\S+)`)
	m := userRe.FindStringSubmatch(content)
	if len(m) > 1 {
		dsn.Username = m[1]
		fmt.Printf("pt-agent MySQL user: %s\n", dsn.Username)
	}
	passRe := regexp.MustCompile(`pass\s*=(\S+)`)
	m = passRe.FindStringSubmatch(content)
	if len(m) > 1 {
		dsn.Password = m[1]
		fmt.Printf("pt-agent MySQL user pass: %s\n", dsn.Password)
	}
	return nil
}

func RemovePTAgent(ptagentConf string) {
	if err := os.Remove(ptagentConf); err != nil {
		fmt.Printf("Warning: failed to remove %s: %s\n", ptagentConf, err)
	}
	if err := os.RemoveAll("/var/lib/pt-agent"); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove /var/lib/pt-agent/: %s\n", err)
	}
	if err := os.RemoveAll("/var/spool/pt-agent"); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove /var/spool/pt-agent/: %s\n", err)
	}
	if err := os.RemoveAll("/etc/percona/agent"); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove /etc/percona/agent/: %s\n", err)
	}
}
