package main

import (
	"encoding/json"
	. "launchpad.net/gocheck"
	"reflect"
	"testing"
	"time"
)

// Hook gocheck into the "go test" runner.
// http://labix.org/gocheck
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
}

var _ = Suite(&TestSuite{})

type HttpClientMock struct {
	GetMock  func(url string, v interface{}, timeout time.Duration) error
	PostMock func(url string, data []byte) error
}

func (h *HttpClientMock) Get(url string, v interface{}, timeout time.Duration) error {
	return h.GetMock(url, v, timeout)
}
func (h *HttpClientMock) Post(url string, data []byte) error {
	return h.Post(url, data)
}

func (s *TestSuite) TestGetApiLinks(c *C) {
	linksJson := `
{
	"Links": {
		"log": "abc",
		"cmd": "123"
	}
}
`
	httpClientMock := &HttpClientMock{
		GetMock: func(url string, v interface{}, timeout time.Duration) error {
			// Actually one can skip passing real json and simply use
			// `links := &apiLinks{map[string]string{ "log": "abc", "cmd": "123" }} `
			// But this way we provide full end to end test with example json
			links := &apiLinks{}
			err := json.Unmarshal([]byte(linksJson), links)
			c.Assert(err, IsNil)

			dataVal := reflect.ValueOf(v).Elem()
			dataVal.Set(reflect.ValueOf(links).Elem())
			return nil
		},
	}
	actualApiLinks, err := GetLinks(httpClientMock, "")
	expectedApiLinks := map[string]string{"log": "abc", "cmd": "123"}

	c.Assert(err, IsNil)
	c.Assert(actualApiLinks, DeepEquals, expectedApiLinks)
}
