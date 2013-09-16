package service

type ServiceIsRunningError struct {
	Service string
}

func (e ServiceIsRunningError) Error() string {
	return e.Service + "service is running"
}

/////////////////////////////////////////////////////////////////////////////

type ServiceIsNotRunningError struct {
	Service string
}

func (e ServiceIsNotRunningError) Error() string {
	return e.Service + "service is not running"
}
