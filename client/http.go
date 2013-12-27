package client

type HttpClient struct {
	apiKey string
}

func NewHttpClient(apiKey string) *HttpClient {
	c := &HttpClient{
		apiKey: apiKey,
	}
	return c
}

func (c *HttpClient) Post(url string, data []byte) error {
	return nil
}
