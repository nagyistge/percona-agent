package proto

import (
	"fmt"
	"log"
	"time"
	"encoding/json"
)

// A command from the API or an agent reply to a command
type Msg struct {
	Ts		time.Time	// sent at (UTC)
	User	string		// by this front-side user
	Id		uint		// back-end routing ID
	Cmd		string		// command (e.g. start-service)
	Timeout uint		// command timeout (seconds)
	Data	[]byte		// command data (e.g. msg.Service) or agent reply (e.g. CmdReply)
}

// Data for StartService and StopService commands
type ServiceMsg struct {
	Name string
	Config []byte `json:",omitempty"`	// e.g. qh.Config (percona-cloud-tools/qh/config.go)
}

// Data for an agent reply to a command
type CmdReply struct {
	Error error // nil if command succeeded
}

// Data for an agent reply to a status command
type StatusReply struct {
	Agent string
	CmdQueue []string			// commands in the queue
	Service map[string]string	// service.Manager.Status()
}

// The API sends messages to which the agent replies.  So there is no NewMsg(), just Reply().
func (msg *Msg) Reply(data interface{}) *Msg {
	codedData, err := json.Marshal(data)
	if err != nil {
		log.Panic(err) // @todo
	}
	reply := &Msg{
		User: msg.User,
		Id: msg.Id,
		Cmd: msg.Cmd,
		Data: codedData,
	}
	return reply
}

func (msg *Msg) String() string {
	return fmt.Sprintf("%s %s %d", msg.Ts, msg.User, msg.Id)
}
