package log

type InvalidLogLevelError struct {
	Level string
}

func (e InvalidLogLevelError) Error() string {
	return "Invalid log level: " + e.Level
}
