package ws_server

// Copied from http://gary.beagledreams.com/page/go-websocket-chat.html

import (
	"code.google.com/p/go.net/websocket"
	"log"
	"net/http"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type MockWsServer struct {
	addr string
}

/*
 * addr:	 http://127.0.0.1:8000
 * endpoint: /, /agent, etc.
 * fromClients:	 fromClients from clients
 */
func (s *MockWsServer) Run(addr string, endpoint string, fromClients chan *proto.Msg, toClients chan *proto.Msg) {
	go h.run(fromClients, toClients)
	http.Handle(endpoint, websocket.Handler(wsHandler))
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
