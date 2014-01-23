package qan

import (
	mysqlLog "github.com/percona/percona-go-mysql/log"
	"github.com/percona/percona-go-mysql/log/parser"
	"os"
	"time"
)

type Job struct {
	SlowLogFile    string
	RunTime        time.Duration
	StartOffset    int64
	StopOffset     int64
	ExampleQueries bool
}

type Result struct {
	Global  *mysqlLog.GlobalClass
	Classes []*mysqlLog.QueryClass
}

type Worker interface {
	Run(job *Job) (*Result, error)
}

type WorkerFactory interface {
	Make() Worker
}

type SlowLogWorker struct {
}

type SlowLogWorkerFactory struct {
}

func NewSlowLogWorkerFactory() *SlowLogWorkerFactory {
	f := &SlowLogWorkerFactory{}
	return f
}

func (f *SlowLogWorkerFactory) Make() Worker {
	return NewSlowLogWorker()
}

func NewSlowLogWorker() Worker {
	w := &SlowLogWorker{}
	return w
}

func (w *SlowLogWorker) Run(job *Job) (*Result, error) {

	// Open the slow log file.
	file, err := os.Open(job.SlowLogFile)
	if err != nil {
		return nil, err
	}

	// Seek to the start offset, if any.
	// @todo error if start off > file size
	if job.StartOffset != 0 {
		// @todo handle error
		file.Seek(int64(job.StartOffset), os.SEEK_SET)
	}

	// Create a slow log parser and run it.  It sends events log events
	// via its channel.
	p := parser.NewSlowLogParser(file, false) // false=debug off
	if err != nil {
		return nil, err
	}
	go p.Run()

	// The global class has info and stats for all events.
	// Each query has its own class, defined by the checksum of its fingerprint.
	global := mysqlLog.NewGlobalClass()
	queries := make(map[string]*mysqlLog.QueryClass)
	for event := range p.EventChan {
		if int64(event.Offset) > job.StopOffset {
			break
		}

		// Add the event to the global class.
		global.AddEvent(event)

		// Get the query class to which the event belongs.
		fingerprint := mysqlLog.Fingerprint(event.Query)
		classId := mysqlLog.Checksum(fingerprint)
		class, haveClass := queries[classId]
		if !haveClass {
			class = mysqlLog.NewQueryClass(classId, fingerprint)
			queries[classId] = class
		}

		// Add the event to its query class.
		class.AddEvent(event)
	}

	// Done parsing the slow log.  Finalize the global and query classes (calculate
	// averages, etc.).
	for _, class := range queries {
		class.Finalize()
	}
	global.Finalize(uint64(len(queries)))

	nQueries := len(queries)
	classes := make([]*mysqlLog.QueryClass, nQueries)
	for _, class := range queries {
		// Decr before use; can't classes[--nQueries] in Go.
		nQueries--
		classes[nQueries] = class
	}

	result := &Result{
		Global:  global,
		Classes: classes,
	}

	return result, nil
}
