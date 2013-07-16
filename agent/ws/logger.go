package ws

// Websocket implementation of the agnet/logger interface

type WsLogger struct {
	Level uint8
	input chan string
	buffer []string
}

func (l *WsLogger) SetLogLevel() {
}

func (l *WsLogger) Debug() {
}

func (l *WsLogger) Info() {
}

func (l *WsLogger) Warn() {
}

func (l *WsLogger) Err() {
}

func (l *WsLogger) Fatal() {
}


