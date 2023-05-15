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

func (w *LogWorker) ProcessJob(ctx context.Context, job Job[Log]) error {
	var (
		listener Listener
		err      error
		ok       bool
	)

	listener, ok = w.listeners[job.Name]
	if !ok {
		log.Info("[Worker] listener not found", "name", job.Name, "event", job.Event, "type", "Log")
		return nil
	}

	if err = listener.TriggerLog(ctx, job.Event, job.Data); err != nil {
		return err
	}

	return nil
}

type TransactionWorker struct {
	id        int
	listeners map[string]Listener
}

func NewTransactionWorker(id int, listeners map[string]Listener) Worker[Transaction] {
	return &TransactionWorker{}
}

func (w *TransactionWorker) ID() int {
	return w.id
}

func (w *TransactionWorker) ProcessJob(ctx context.Context, job Job[Transaction]) error {
	var (
		listener Listener
		err      error
		ok       bool
	)

	listener, ok = w.listeners[job.Name]
	if !ok {
		log.Info("[Worker] listener not found", "name", job.Name, "event", job.Event, "type", "Transaction")
		return nil
	}

	if err = listener.TriggerTransaction(ctx, job.Event, job.Data); err != nil {
		return err
	}

	return nil
}
