package mock

type Spooler struct {
	FilesOut []string          // test provides
	DataOut  map[string][]byte // test provides
	DataIn   []interface{}
}

func NewSpooler() *Spooler {
	s := &Spooler{
		DataIn: []interface{}{},
	}
	return s
}

func (s *Spooler) Start() error {
	return nil
}

func (s *Spooler) Stop() error {
	return nil
}

func (s *Spooler) Write(data interface{}) error {
	s.DataIn = append(s.DataIn, data)
	return nil
}

func (s *Spooler) Files() <-chan string {
	filesChan := make(chan string)
	go func() {
		for _, file := range s.FilesOut {
			filesChan <- file
		}
		close(filesChan)
	}()
	return filesChan
}

func (s *Spooler) Read(file string) ([]byte, error) {
	return s.DataOut[file], nil
}

func (s *Spooler) Remove(file string) error {
	delete(s.DataOut, file)
	return nil
}
