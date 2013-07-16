package proto

type Client interface {
	NewClient(url string, endpoint string) (*Client, error)
	Connect() error
	Disconnect() error
	Send(msg *Msg) error
	Recv() (*Msg, error)
}
