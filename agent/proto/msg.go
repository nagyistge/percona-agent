package proto

import (
	"encoding/json"
)

// The simple, generic struct of all messages.
type Msg struct {
	Command string
	Data string
}

/*
 * Static messages, not exported.  Use their exported func counterparts.
 */

var ok = Msg{
	Command: "ok",
	Data: "",
}

var ack = Msg{
	Command: "ack",
	Data: "",
}

var exit = Msg{
	Command: "exit",
	Data: "",
}

var ping = Msg{
	Command: "ping",
	Data: "",
}

var pong = Msg{
	Command: "pong",
	Data: "",
}

/*
 * Exported functions
 */

 /*
  * var data map[string]string
  * data["msg"] = "It crashed!"
  * client.Send( NewMsg("err", data) )
  */
func NewMsg(command string, mapData interface{}) *Msg {
	codedData, err := json.Marshal(mapData)
	if err != nil {
		// todo
	}
	var msg = Msg{
		Command: command,
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
