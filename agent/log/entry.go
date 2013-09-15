package log

const (
	LOG_LEVEL_FATAL = 5
	LOG_LEVEL_ERROR = 4
	LOG_LEVEL_WARN  = 3
	LOG_LEVEL_INFO  = 2
	LOG_LEVEL_DEBUG = 1
)

type LogEntry struct {
	User string `json:",omitempty"`
	Id uint `json:",omitempty"`
	Level uint
	Service string
	Msg string
}
