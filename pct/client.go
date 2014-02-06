package pct

import (
	"code.google.com/p/go.net/websocket"
	"github.com/percona/cloud-protocol/proto"
)

type WebsocketClient interface {
	Connect()
	ConnectOnce() error
	Disconnect() error

	// Channel interface:
	Start()
	Stop()
	SendChan() chan *proto.Reply
	RecvChan() chan *proto.Cmd
	ConnectChan() chan bool
	ErrorChan() chan error

	// Direct interface:
	SendBytes(data []byte) error
	Send(data interface{}) error
	Recv(data interface{}, timeout uint) error
	Conn() *websocket.Conn
}
