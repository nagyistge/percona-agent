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

type WsClient struct {
	url string
	endpoint string
	config *websocket.Config
	conn *websocket.Conn
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
	}
	return c, nil
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

func (c *WsClient) Send(msg *proto.Msg) error {
	err := websocket.JSON.Send(c.conn, msg)
	return err
}

func (c *WsClient) Recv(msg *proto.Msg) error {
	err := websocket.JSON.Receive(c.conn, msg)
	return err
}
