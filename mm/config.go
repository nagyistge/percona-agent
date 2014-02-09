package mm

const CONFIG_FILE = "mm.conf"

type Interval struct {
	Collect uint // how often monitor collects metrics (seconds)
	Report  uint // how often aggregator reports metrics (seconds)
}

type Config struct {
	Intervals map[string]Interval // e.g. mysql=>{Collect:1, Report:60}
}
