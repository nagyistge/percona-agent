package ws

// Websocket implementation of the agent/proto/client interface

type WsClient struct {
	addr string
	ws *websocket.Conn
}

func (client *WsClient) NewClient(addr string) {
}

func (client *WsClient) Connect() {
}

func (client *WsClient) Disconnect() {
}

func (client *WsClient) Send(msg *Msg) {
}

func (c.ient *WsClient) Recv(msg *Msg) {
}
