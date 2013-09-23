package mock

import (
	"github.com/percona/percona-cloud-tools/agent/proto"
)

type NullClient struct {
}

func (c *NullClient) Connect() error {
	return nil
}

func (c *NullClient) Run() {
}

func (c *NullClient) Disconnect() error {
	return nil
}

func (c *NullClient) Send(data interface{}) error {
	return nil
}

func (c *NullClient) Recv(data interface{}) error {
	return nil
}

func (c *NullClient) SendChan() chan *proto.Msg {
	return nil
}

func (c *NullClient) RecvChan() chan *proto.Msg {
	return nil
}
