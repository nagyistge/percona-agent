package test

import (
	proto "github.com/percona/cloud-protocol"
	"time"
)

func WaitCmd(replyChan chan *proto.Cmd) []proto.Cmd {
	var buf []proto.Cmd
	var haveData bool = true
	for haveData {
		select {
		case cmd := <-replyChan:
			buf = append(buf, *cmd)
		case <-time.After(100 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitReply(replyChan chan *proto.Reply) []proto.Reply {
	var buf []proto.Reply
	var haveData bool = true
	for haveData {
		select {
		case reply := <-replyChan:
			buf = append(buf, *reply)
		case <-time.After(100 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitData(recvDataChan chan interface{}) []interface{} {
	var buf []interface{}
	var haveData bool = true
	for haveData {
		select {
		case data := <-recvDataChan:
			buf = append(buf, data)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitLog(recvDataChan chan interface{}) []proto.LogEntry {
	var buf []proto.LogEntry
	var haveData bool = true
	for haveData {
		select {
		case data := <-recvDataChan:
			buf = append(buf, *data.(*proto.LogEntry))
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}

func WaitTrace(traceChan chan string) []string {
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

func WaitErr(errChan chan error) []error {
	var buf []error
	var haveData bool = true
	for haveData {
		select {
		case err := <-errChan:
			buf = append(buf, err)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}
