package data

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
)

type Serializer interface {
	ToBytes(data interface{}) ([]byte, error)
	Encoding() string
	FileType() string
}

type JsonGzipSerializer struct {
	e *json.Encoder
	g *gzip.Writer
	b *bytes.Buffer
}

func NewJsonGzipSerializer() *JsonGzipSerializer {
	b := &bytes.Buffer{}    // 4. buffer
	g := gzip.NewWriter(b)  // 3. gzip
	e := json.NewEncoder(g) // 2. encode
	// ....................... 1. data

	s := &JsonGzipSerializer{
		e: e,
		g: g,
		b: b,
	}
	return s
}

func (s *JsonGzipSerializer) ToBytes(data interface{}) ([]byte, error) {
	s.b.Reset()
	s.g.Reset(s.b)
	if err := s.e.Encode(data); err != nil {
		return nil, err
	}
	s.g.Close()
	return s.b.Bytes(), nil
}

func (s *JsonGzipSerializer) Encoding() string {
	return "gzip"
}

func (s *JsonGzipSerializer) FileType() string {
	return "gz"
}

// --------------------------------------------------------------------------

type JsonSerializer struct {
}

func NewJsonSerializer() *JsonSerializer {
	j := &JsonSerializer{}
	return j
}

func (j *JsonSerializer) ToBytes(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

func (s *JsonSerializer) Encoding() string {
	return ""
}

func (s *JsonSerializer) FileType() string {
	return "json"
}
