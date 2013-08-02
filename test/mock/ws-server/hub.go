package ws_server

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type hub struct {
	connections map[*connection]bool
	broadcast chan *proto.Msg
	register chan *connection
	unregister chan *connection
}

var h = hub{
	broadcast:   make(chan *proto.Msg),
	register:    make(chan *connection),
	unregister:  make(chan *connection),
	connections: make(map[*connection]bool),
}

func (h *hub) run(fromClients chan *proto.Msg, toClients chan *proto.Msg) {
	for {
		select {
		case c := <-h.register:
			h.connections[c] = true
		case c := <-h.unregister:
			delete(h.connections, c)
			close(c.send)
		case msg := <-h.broadcast:
			fromClients <- msg
		case msg := <-toClients:
			for c := range h.connections {
				select {
				case c.send <- msg:
				default:
					delete(h.connections, c)
					close(c.send)
					go c.ws.Close()
				}
			}
		}
	}
}
