package agent

type UnknownServiceError struct {
	Service string
}

func (e UnknownServiceError) Error() string {
	return "Agent does not have a service manager for" + e.Service
}

type CmdTimeoutError struct {
	Cmd string
}

func (e CmdTimeoutError) Error() string {
	return "Timeout waiting for " + e.Cmd
}
