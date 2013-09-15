package testapp

import (
	"time"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"github.com/percona/percona-cloud-tools/agent/log"
)

/*
 * Tests that use clients and check what those clients send need to wait for
 * the messages from the client because the test and the client are concurrent
 * go routines.
 */
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
