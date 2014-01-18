package mysql

type Config struct {
	DSN              string   // [username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
	InstanceName     string   // optional name of MySQL instance
	Status           map[string]byte // SHOW STATUS variables to collect, case-sensitive
	InnoDB           string
	Userstat         bool
	UserstatIgnoreDb string
}
