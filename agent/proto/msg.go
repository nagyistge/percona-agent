package proto

import (
	"log"
	"encoding/json"
)

// The simple, generic struct of all messages.
type Msg struct {
	Cmd  string `json:"cmd"`
	Data string `json:"data,omitempty"`
}

// Methods

 /*
  * var data map[string]string
  * data["msg"] = "It crashed!"
  * client.Send( NewMsg("err", data) )
  */
func NewMsg(cmd string, data interface{}) *Msg {
	codedData, err := json.Marshal(data)
	if err != nil {
		log.Panic(err) // todo
	}
	var msg = Msg{
		Cmd: cmd,
		Data: string(codedData),  // map -> bytes -> string
	}
	return &msg
}

// E.g. client.Send(Ack())
func Ack() *Msg {
	return &ack
}

func Ok() *Msg {
	return &ok
}

func Exit() *Msg {
	return &exit
}

func Ping() *Msg {
	return &ping
}

func Pong() *Msg {
	return &pong
}

/*
 * Static messages, not exported.  Use their exported func counterparts.
 */

var ok = Msg{
	Cmd: "ok",
}

var ack = Msg{
	Cmd: "ack",
}

var exit = Msg{
	Cmd: "exit",
}

// Ping-pong is bidirectional: client can ping server and server can ping client.
var ping = Msg{
	Cmd: "ping",
}

var pong = Msg{
	Cmd: "pong",
}
