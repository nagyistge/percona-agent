package testapp

import (
	"time"
	"github.com/percona/percona-cloud-tools/agent/proto"
)

/*
 * Tests that use clients and check what those clients send need to wait for
 * the messages from the client because the test and the client are concurrent
 * go routines.
 */
func WaitForClientMsgs(fromClients chan *proto.Msg) []proto.Msg {
	var buf []proto.Msg
	var haveData bool = true
	for haveData {
		select {
		case msg := <-fromClients:
			buf = append(buf, *msg)
		case <-time.After(10 * time.Millisecond):
			haveData = false
		}
	}
	return buf
}
