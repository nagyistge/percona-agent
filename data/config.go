package data

const (
	DEFAULT_DATA_DIR           = "/var/spool/percona"
	DEFAULT_DATA_ENCODING      = ""
	DEFAULT_DATA_SEND_INTERVAL = 63
)

type Config struct {
	Dir          string
	Encoding     string
	SendInterval int
}
