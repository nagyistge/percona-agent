package log

const (
	DEFAULT_LOG_FILE  = ""
	DEFAULT_LOG_LEVEL = "info"
)

type Config struct {
	Level   string
	File    string
	Offline bool
}
