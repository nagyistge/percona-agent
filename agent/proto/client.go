package proto

type Client interface {
	Connect()
	Disconnect()
	Send(msg *Msg)
	Recv(msg *Msg)
}
