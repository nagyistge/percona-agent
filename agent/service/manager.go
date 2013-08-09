package service

type Manager interface {
	Start(config []byte) error
	Stop() error
	UpdateConfig(config []byte) error
	Status() string
	IsRunning() bool
}
