package main

import (
	"flag"
	"log"
	"fmt"
	"code.google.com/p/go.net/websocket"
)

// Use -addr to change bind address:port.
var addr = flag.String("addr", "ws://127.0.0.1:8000", "http service address")

func main() {
	origin := "http://localhost"
	ws, err := websocket.Dial(*addr, "", origin)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Client connected to ", addr)

	go func() {
		fmt.Println("Receiving...")
		var resp string
		websocket.JSON.Receive(ws, &resp)
		fmt.Printf("Received '%s'\n", resp)
	}()

	for {
		var s string
		fmt.Scan(&s)
		fmt.Printf("Sending '%s'\n", s)
		websocket.JSON.Send(ws, s)

	}
}
