package mock

import (
	"time"
)

type HttpClient struct {
	PostChan chan []byte
}

func (c *HttpClient) Get(url string, v interface{}, timeout time.Duration) error {
	return nil
}

func (c *HttpClient) Post(url string, data []byte) error {
	c.PostChan <- data
	return nil
}
