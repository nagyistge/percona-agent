package mysql

type Config struct {
	CollectInterval   uint     // 1s
	ReportInterval    uint     // 60s (aggregated)
	DSN               string   // [username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
	InstanceName      string   // optional name of MySQL instance
	Status            []string // SHOW STATUS variables to collect, case-sensitive
	Var               []string // SHOW VARIABLES variables to collect, case-sensitive
	InnoDBMetrics     string
	Userstats         bool
	UserstatsIgnoreDb string
}
