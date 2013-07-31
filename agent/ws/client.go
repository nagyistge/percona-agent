package ws

import (
	//"fmt"
	"log"
	"encoding/json"
)

// Websocket implementation of the agent/proto/client interface

import (
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
		// todo
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
		log.Print(err)
		// todo
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
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Print(err)
		return err
	}
	_, err = c.conn.Write(bytes)
	if err != nil {
		log.Print(err)
		return err
	}
	return nil
}

func (c *WsClient) Recv() (*proto.Msg, error) {
	return nil, nil
}
