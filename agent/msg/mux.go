package msg

// proto/msg multiplexer

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

const (
	MUX_SEND = -1
	MUX_IDLE = 0
	MUX_RECV = 1
)

type Mux struct {
	client proto.Client
	ctrlChan chan bool
	sendChan chan *proto.Msg
	recvChan chan *proto.Msg
}

func (m *Mux) Run() {
	go m.Recv()
}

func (m *Mux) Send(msg *Msg) {
}

func (m *Mux) Recv() {
	for {
		msg , err := m.client.Recv()
		if err != nil {
			// todo
		}
		m.recvChan <- msg
	}
}
