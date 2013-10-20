package test

import (
	"time"
	golog "log"
	"encoding/json"
	"github.com/percona/percona-cloud-tools/agent"
	"github.com/percona/percona-cloud-tools/agent/log"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

func DoneWait(cc *agent.ControlChannels) bool {
	// Tell whatever to stop...
	cc.StopChan <-true

	// Then wait for it.  The wait is necessary to yield what's probably
	// a single thread running the caller and the thing they're waiting
	// for.  If we just <-cc.DoneChan, the calling thread will block, never
	// letting the other thing cc.DoneChan <-true.  Concurrency is fun!
	golog.SetFlags(golog.LstdFlags | golog.Lmicroseconds)
	select {
	case <-cc.DoneChan:
		return true
	case <-time.After(250 * time.Millisecond):
		return false
	}
	return false
}

func WaitForClientMsgs(msgFromClient chan *proto.Msg) []proto.Msg {
	var buf []proto.Msg
	var haveData bool = true
	for haveData {
		select {
		case msg := <-msgFromClient:
			buf = append(buf, *msg)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitForLogEntries(dataFromClient chan interface{}) []log.LogEntry {
	var buf []log.LogEntry
	var haveData bool = true
	for haveData {
		select {
		case data := <-dataFromClient:
			buf = append(buf, *data.(*log.LogEntry))
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitForTraces(traceChan chan string) []string {
	var buf []string
	var haveData bool = true
	for haveData {
		select {
		case msg := <-traceChan:
			buf = append(buf, msg)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func GetStatus(toClient chan *proto.Msg, fromClient chan *proto.Msg) *proto.StatusReply {
	statusCmd := &proto.Msg{
		Ts: time.Now(),
		User: "user",
		Id: 1,
		Cmd: "Status",
		Timeout: 3,
	}
	toClient <-statusCmd

	reply := new(proto.StatusReply)
	select {
	case msg := <-fromClient:
		_ = json.Unmarshal(msg.Data, reply)
		return reply
	case <-time.After(10 * time.Millisecond):
	}

	return reply
}

// @todo replace with WaitForLogEntries()
func GetLogEntries(cc *agent.ControlChannels) []log.LogEntry {
	var buf []log.LogEntry
	var haveData bool = true
	for haveData {
		select {
		case msg := <-cc.LogChan:
			buf = append(buf, *msg)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}
