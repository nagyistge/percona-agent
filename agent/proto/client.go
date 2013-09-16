package proto

// See percona-cloud-tools/agent/ws/client.go for an implementation of this interface.

type Client interface {
	// Call Client.Run() then send and receive proto.Msg on these channels:
	Run()
	RecvChan() chan *Msg
	SendChan() chan *Msg

	// Low-level interface for sending log.LogEntry and service data:
	Connect() error
	Disconnect() error
	Send(interface{}) error
	Recv(interface{}) error
}
