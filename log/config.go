package log

const (
	DEFAULT_LOG_FILE  = "/var/log/percona/agent.log"
	DEFAULT_LOG_LEVEL = "info"
)

type Config struct {
	Level string
	File  string
}
