package ws_server

import (
	"log"
	"code.google.com/p/go.net/websocket"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

// A client websocket connect
type connection struct {
	ws *websocket.Conn
	send chan *proto.Msg // data to send to client
}

// Called when a new client connects
func wsHandler(ws *websocket.Conn) {
	// Create a new websocket connection for this client.
	c := &connection{
		ws: ws,
		send: make(chan *proto.Msg, 10),
	}
	// Register the connection with the hub.
	h.register <- c
	// Unregister the connection when it closes.
	defer func() { h.unregister <- c }()
	// Send message from hub to client (via send chan).
	go c.writer()
	// Send messages from client to hub (via ws connection to client).
	c.reader()
}

func (c *connection) reader() {
	for {
		msg := new(proto.Msg)
		err := websocket.JSON.Receive(c.ws, msg)
		if err != nil {
			break
		}
		h.broadcast <- msg
	}
	c.ws.Close()
}

func (c *connection) writer() {
	for msg := range c.send {
		err := websocket.JSON.Send(c.ws, msg)
		if err != nil {
			log.Print(err)
		}
	}
	c.ws.Close()
}

