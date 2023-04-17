package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Worker interface {
	ID() int
	ProcessJob(ctx context.Context, job Job) error
}

type BridgeWorker struct {
	ctx context.Context
	id  int
	// workerChan is used to receive and process job
	workerChan     chan Job
	listeners      map[string]Listener
	eventStreaming *EventStreaming
}

func NewBridgeWorker(ctx context.Context, id int, listeners map[string]Listener) Worker {
	return &BridgeWorker{
		ctx:       ctx,
		id:        id,
		listeners: listeners,
	}
}

func (w *BridgeWorker) ID() int {
	return w.id
}

func (w *BridgeWorker) String() string {
	return fmt.Sprintf("{ id: %d, currentSize: %d }", w.id, len(w.workerChan))
}

func (w *BridgeWorker) ProcessJob(ctx context.Context, job Job) error {
	var (
		payload map[string]interface{}

		listener Listener
		event    string
		ok       bool

		args []interface{}
	)
	payload = job.Payload()
	listener, ok = payload["Listener"].(Listener)
	if !ok {
		return errors.New("listener is not found")
	}

	event, ok = payload["Event"].(string)
	if !ok {
		return errors.New("event is not found")
	}

	data, _ := json.Marshal(args)

	w.eventStreaming.Publish(ctx, event, data)
	// if job.GetType() == ListenHandler && job.GetSubscriptionName() != "" {
	// 	job.GetListener().SendCallbackJobs(w.listeners, job.GetSubscriptionName(), job.GetTransaction(), val)
	// }
	return nil
}
