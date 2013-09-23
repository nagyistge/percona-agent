package test

import (
	"time"
	"github.com/percona/percona-cloud-tools/agent"
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

func DoneWait(cc *agent.ControlChannels) bool {
	// Tell whatever to stop...
	cc.StopChan <-true

	// Then wait for it.  The wait is necessary to yield what's probably
	// a single thread running the caller and the thing they're waiting
	// for.  If we just <-cc.DoneChan, the calling thread will block, never
	// letting the other thing cc.DoneChan <-true.  Concurrency is fun!
	select {
	case <-cc.DoneChan:
		return true
	case <-time.After(250 * time.Millisecond):
		return false
	}
	return false
}
