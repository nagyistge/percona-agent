package logrelay

import (
	"fmt"
	proto "github.com/percona/cloud-protocol"
	golog "log"
	"os"
	"time"
)

const (
	BUFFER_SIZE = 10
)

type LogRelay struct {
	client        proto.WebsocketClient
	connectedChan chan bool
	connected     bool
	logChan       chan *proto.LogEntry
	logLevel      int
	logLevelChan  chan int
	logFile       *golog.Logger
	logFileChan   chan string
	firstBuf      []*proto.LogEntry
	secondBuf     []*proto.LogEntry
	lost          uint
}

/**
 * client is optional.  If not given, only file logging is enabled if a log file
 * is sent to the LogFileChan().
 */
func NewLogRelay(client proto.WebsocketClient) *LogRelay {
	r := &LogRelay{
		client:        client,
		logLevel:      proto.LOG_NOTICE,
		logLevelChan:  make(chan int),
		logChan:       make(chan *proto.LogEntry, BUFFER_SIZE*2),
		logFileChan:   make(chan string),
		firstBuf:      make([]*proto.LogEntry, BUFFER_SIZE),
		secondBuf:     make([]*proto.LogEntry, BUFFER_SIZE),
		connectedChan: make(chan bool, 2),
	}
	return r
}

func (r *LogRelay) LogChan() chan *proto.LogEntry {
	return r.logChan
}

func (r *LogRelay) LogLevelChan() chan int {
	return r.logLevelChan
}

func (r *LogRelay) LogFileChan() chan string {
	return r.logFileChan
}

// @goroutine
func (r *LogRelay) Run() {
	// Connect if we were created with a client.  If this is slow, log entries
	// will be buffered and sent later.
	go r.connect()

	for {
		select {
		case entry := <-r.logChan:
			if entry.Level > r.logLevel {
				// Log level too high, too verbose; ignore.
				continue
			}

			if r.client != nil {
				if r.connected {
					// Send log entry to API.
					r.send(entry, true) // buffer on err
				} else {
					// API not available right now, buffer and try later on reconnect.
					r.buffer(entry)
				}
			}

			if r.logFile != nil {
				// Write log entry to file, too.
				r.logFile.Println(entry)
			}
		case connected := <-r.connectedChan:
			if connected {
				// Connected for first time or reconnected.
				if len(r.firstBuf) > 0 {
					// Send log entries we saved while offline.
					r.resend()
				}
			} else {
				// waitErr() returned, so we got an error on websocket recv,
				// probably due to lost connection to API.  Keep trying to
				// reconnect in background, buffering log entries while offline.
				go r.connect()
			}
		case file := <-r.logFileChan:
			r.setLogFile(file)
		case level := <-r.logLevelChan:
			r.setLogLevel(level)
		}
	}
}

// Even the relayer needs to log stuff.  Hopefully this won't cause an infinite loop?
func (r *LogRelay) internal(msg string) {
	logEntry := &proto.LogEntry{
		Level: proto.LOG_WARNING,
		Msg:   msg,
	}
	r.logChan <- logEntry
}

// @goroutine
func (r *LogRelay) connect() {
	if r.client == nil {
		// log file only
		return
	}
	r.client.Connect()
	r.connected = true
	r.connectedChan <- true
	go r.waitErr()
}

// @goroutine
func (r *LogRelay) waitErr() {
	// When a websocket closes, the err is returned on recv,
	// so we block on recv, not expecting any data, just
	// waiting for error/disconenct.
	var data interface{}
	if err := r.client.Recv(data); err != nil {
		r.internal("API lost")
		r.connectedChan <- false
	}
}

func (r *LogRelay) buffer(e *proto.LogEntry) {
	// First time we need to buffer delayed/lost log entries is closest to
	// the events that are causing problems, so we keep some, and when this
	// buffer is full...
	n := len(r.firstBuf)
	if n < BUFFER_SIZE {
		r.firstBuf[n] = e
		return
	}

	// ...we switch to second, sliding window buffer, keeping the latest
	// log entries and a tally of how many we've had to drop from the start
	// (firstBuf) until now.
	n = len(r.secondBuf)
	if n < BUFFER_SIZE {
		r.secondBuf[n] = e
		return
	}

	// secondBuf is full too.  This problem is long-lived.  Throw away the
	// buf and keep saving the latest log entries, counting how many we've lost.
	r.lost += BUFFER_SIZE
	for i := 0; i < BUFFER_SIZE; i++ {
		r.secondBuf[i] = nil
	}
	r.secondBuf[0] = e
}

func (r *LogRelay) send(entry *proto.LogEntry, bufferOnErr bool) error {
	var err error
	r.client.Conn().SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err = r.client.Send(entry); err != nil {
		if bufferOnErr {
			r.buffer(entry)
		}
	}
	return err
}

func (r *LogRelay) resend() {
	for i := 0; i < BUFFER_SIZE; i++ {
		if r.firstBuf[i] != nil {
			if err := r.send(r.firstBuf[i], false); err == nil {
				r.firstBuf[i] = nil
			}
		}
	}

	if r.lost > 0 {
		r.internal(fmt.Sprintf("Lost %d log entries", r.lost))
		r.lost = 0
	}

	for i := 0; i < BUFFER_SIZE; i++ {
		if r.secondBuf[i] != nil {
			if err := r.send(r.secondBuf[i], false); err == nil {
				r.firstBuf[i] = nil
			}
		}
	}
}

func (r *LogRelay) setLogLevel(level int) {
	if level < proto.LOG_EMERGENCY || level > proto.LOG_DEBUG {
		r.internal(fmt.Sprintf("Invalid log level: %d\n", level))
	} else {
		r.logLevel = level
	}
}

func (r *LogRelay) setLogFile(logFile string) {
	file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		r.internal(err.Error())
	} else {
		logFile := golog.New(file, "", golog.LstdFlags)
		r.logFile = logFile
	}
}
