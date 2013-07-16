package main

// Copied from http://gary.beagledreams.com/page/go-websocket-chat.html

import (
	"code.google.com/p/go.net/websocket"
	"flag"
	"log"
	"net/http"
)

// Use -addr to change bind address:port.
var addr = flag.String("addr", "127.0.0.1:8000", "http service address")

func main() {
	flag.Parse()

	// Start the central connection hub.
	go h.run()

	http.Handle("/", websocket.Handler(wsHandler))
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
