package types

import (
	"context"

	"github.com/ethereum/go-ethereum/log"
)

type Worker[T any] interface {
	ID() int
	ProcessJob(ctx context.Context, job Job[T]) error
}

type LogWorker struct {
	id        int
	listeners map[string]Listener
}

func NewLogWorker(id int, listeners map[string]Listener) Worker[Log] {
	return &LogWorker{
		id:        id,
		listeners: listeners,
	}
}

func (w *LogWorker) ID() int {
	return w.id
}

var (
	listener Listener
	err      error
	ok       bool
)

func (w *LogWorker) ProcessJob(ctx context.Context, job Job[Log]) error {

	listener, ok = w.listeners[job.Name]
	if !ok {
		log.Info("listener not found")
		return nil
	}

	if err = listener.TriggerLog(ctx, job.Event, job.Data); err != nil {

		return err
	}

	return nil
}
