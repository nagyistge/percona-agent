package service

type Manager interface {
	NewManager()
	Start(interface{}) error
	Stop() error
	Status() string
	IsRunning() bool
}
