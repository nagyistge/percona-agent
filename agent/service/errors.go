package service

type ServiceIsRunningError struct {
	Service string
}

func (e ServiceIsRunningError) Error() string {
	return e.Service + "service is already configured and running"
}
