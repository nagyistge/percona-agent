package client

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"
)

type HttpClient struct {
	apiKey string
}

func NewHttpClient(apiKey string) *HttpClient {
	c := &HttpClient{
		apiKey: apiKey,
	}
	return c
}

func (c *HttpClient) Get(url string, v interface{}, timeout time.Duration) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}

func (c *HttpClient) Post(url string, data []byte) error {
	return nil
}
