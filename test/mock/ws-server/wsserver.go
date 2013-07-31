package ws_server

// Copied from http://gary.beagledreams.com/page/go-websocket-chat.html

import (
	"code.google.com/p/go.net/websocket"
	"log"
	"net/http"
)

type MockWsServer struct {
	addr string
}

/*
 * addr:	 http://127.0.0.1:8000
 * endpoint: /, /agent, etc.
 * data:	 data from clients
 */
func (s *MockWsServer) Run(addr string, endpoint string, data chan string, done chan bool) {
	go h.run(data, done)
	http.Handle(endpoint, websocket.Handler(wsHandler))
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
