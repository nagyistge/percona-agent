package proto

type Client interface {
	Connect() error
	Disconnect() error
	Send(msg *Msg) error
	Recv() (*Msg, error)
}
