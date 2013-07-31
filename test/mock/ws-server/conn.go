package ws_server

import (
	//"fmt"
	//"log"
	"code.google.com/p/go.net/websocket"
)

// A client websocket connect
type connection struct {
	ws *websocket.Conn
	send chan string  // data to send to client
}

// Called when a new client connects
func wsHandler(ws *websocket.Conn) {
	// Create a new websocket connection for this client.
	c := &connection{
		ws: ws,
		send: make(chan string, 256),
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
		var message string
		err := websocket.Message.Receive(c.ws, &message)
		if err != nil {
			break
		}
		h.broadcast <- message
	}
	c.ws.Close()
}

func (c *connection) writer() {
	for message := range c.send {
		err := websocket.Message.Send(c.ws, message)
		if err != nil {
			break
		}
	}
	c.ws.Close()
}

