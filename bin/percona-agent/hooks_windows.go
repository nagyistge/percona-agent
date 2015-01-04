// +build windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func hookOnSignals() (chan os.Signal, chan os.Signal) {
	statusSigChan := make(chan os.Signal, 1)
	signal.Notify(statusSigChan, syscall.SIGKILL) // kill -USER1 PID
	reconnectSigChan := make(chan os.Signal, 1)
	signal.Notify(reconnectSigChan, syscall.SIGHUP) // kill -HUP PID
	return statusSigChan, reconnectSigChan
}
