package fakeapi

import (
	"net/http"
	"net/http/httptest"
)

type FakeApi struct {
	testServer *httptest.Server
	serveMux   *http.ServeMux
}

func NewFakeApi() *FakeApi {
	fakeApi := &FakeApi{}
	fakeApi.serveMux = http.NewServeMux()
	fakeApi.testServer = httptest.NewServer(fakeApi.serveMux)
	return fakeApi
}

func (f *FakeApi) Close() {
	f.testServer.Close()
}

func (f *FakeApi) URL() string {
	return f.testServer.URL
}

func (f *FakeApi) Append(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	f.serveMux.HandleFunc(pattern, handler)
}
