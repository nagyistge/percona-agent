package ws

// Websocket implementation of the agent/proto/client interface

/*
 * This is a very thin wrapper around go.net/websocket.  Using our own
 * client interface makes testing easier because we can use a mock client
 * for the agent instead of this real client.
 */

 // todo Handle reconnect

import (
	"log"
	"github.com/percona/percona-cloud-tools/agent/proto"
	"code.google.com/p/go.net/websocket"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

type WsClient struct {
	url string
	endpoint string
	config *websocket.Config
	conn *websocket.Conn
	sendChan chan *proto.Msg
	recvChan chan *proto.Msg
	SendErr error
	RecvErr error
}

func NewClient(url string, endpoint string) (*WsClient, error) {
	config, err := websocket.NewConfig(url + endpoint, "http://localhost")
	if err != nil {
		log.Fatal(err) // todo
	}
	c := &WsClient{
		url: url,
		endpoint: endpoint,
		config: config,
		conn: nil,
		sendChan: make(chan *proto.Msg, SEND_BUFFER_SIZE),
		recvChan: make(chan *proto.Msg, RECV_BUFFER_SIZE),
	}
	return c, nil
}

func (c *WsClient) Run() {
	go func() {
		for msg := range c.sendChan {
			c.SendErr = c.Send(msg)
		}
	}()

	go func() {
		for {
			msg := new(proto.Msg)
			c.RecvErr = c.Recv(msg)
			if c.RecvErr == nil {
				c.recvChan <-msg
			} else { 
				// @todo
				// log.Printf("websocket.Receive error: %s\n", c.RecvErr)
				break
			}
		}
	}()
}

func (c *WsClient) SendChan() chan *proto.Msg {
	return c.sendChan
}

func (c *WsClient) RecvChan() chan *proto.Msg {
	return c.recvChan
}

func (c *WsClient) Connect() error {
	conn, err := websocket.DialConfig(c.config)
	if err != nil {
		log.Print(err) // todo
		return err
	}
	c.conn = conn
	return nil
}

func (c *WsClient) Disconnect() error {
	if c.conn != nil {
		err := c.conn.Close()
		return err
	}
	return nil // not connected
}

func (c *WsClient) Send(data interface{}) error {
	err := websocket.JSON.Send(c.conn, data)
	return err
}

func (c *WsClient) Recv(data interface{}) error {
	err := websocket.JSON.Receive(c.conn, data)
	return err
}
