package proto

import (
	"log"
	"encoding/json"
)

/*
 * Msg is the highest level structure sent by the server to the agent.
 * The agent handles the Cmd by passing the raw Data to a handler method
 * (e.g. startService()).  Each handler method knows the proto/msg/* type
 * of the data and decodes it accordingly.  proto/msg/* types may also
 * service-specif data; see those files for details.
 */
type Msg struct {
	Cmd  string `json:"cmd"`
	Data []byte `json:"data,omitempty"`
}

/////////////////////////////////////////////////////////////////////////////
// Msg factory methods
/////////////////////////////////////////////////////////////////////////////

/*
 * Example:
 *   var data map[string]string
 *   data["msg"] = "It crashed!"
 *   agent.client.Send(proto.NewMsg("err", data))
 */

func NewMsg(cmd string, data interface{}) *Msg {
	codedData, err := json.Marshal(data)
	if err != nil {
		log.Panic(err) // todo
	}
	var msg = Msg{
		Cmd: cmd,
		Data: codedData,  // map -> []byte
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
