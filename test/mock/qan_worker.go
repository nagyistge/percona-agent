package mock

import (
	"github.com/percona/cloud-tools/qan"
)

type QanWorkerFactory struct {
	workers  []*QanWorker
	workerNo int
}

func NewQanWorkerFactory(workers []*QanWorker) qan.WorkerFactory {
	f := &QanWorkerFactory{
		workers: workers,
	}
	return f
}

func (f *QanWorkerFactory) Make() qan.Worker {
	if f.workerNo > len(f.workers) {
		return f.workers[f.workerNo-1]
	}
	nextWorker := f.workers[f.workerNo]
	f.workerNo++
	return nextWorker
}

type QanWorker struct {
	stopChan chan bool
	result   *qan.Result
	err      error
	// --
	runningChan chan bool
	Job         *qan.Job
}

func NewQanWorker(stopChan chan bool, result *qan.Result, err error) *QanWorker {
	w := &QanWorker{
		stopChan:    stopChan,
		result:      result,
		err:         err,
		runningChan: make(chan bool, 1),
	}
	return w
}

func (w *QanWorker) Run(job *qan.Job) (*qan.Result, error) {
	w.Job = job
	w.runningChan <- true

	// Pretend like we're running until test says to stop.
	<-w.stopChan

	return w.result, w.err
}

func (w *QanWorker) Running() chan bool {
	return w.runningChan
}
