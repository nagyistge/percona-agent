package pct

import (
	"code.google.com/p/go.net/websocket"
	"github.com/percona/cloud-protocol/proto"
)

type WebsocketClient interface {
	Connect() error
	Disconnect() error

	// Non-blocking cmd/reply channels:
	Run()
	RecvChan() chan *proto.Cmd
	SendChan() chan *proto.Reply

	// Blocking calls for logger:
	Send(data interface{}) error
	Recv(data interface{}) error

	// Notify user to stop or reconnect:
	ErrorChan() chan error
	Conn() *websocket.Conn
}

type HttpClient interface {
	Post(url string, data []byte) error
}
