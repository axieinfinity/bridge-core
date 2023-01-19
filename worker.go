package bridge_core

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/log"
	"github.com/go-stack/stack"
	"sync/atomic"
)

type Worker interface {
	Context() context.Context
	Close()
	ProcessJob(job JobHandler) error
	IsClose() bool
	Channel() chan JobHandler
}

type BridgeWorker struct {
	ctx context.Context
	id  int
	// workerChan is used to receive and process job
	workerChan chan JobHandler
	listeners  map[string]Listener
	isClose    int32
}

func NewWorker(ctx context.Context, id int, size int, listeners map[string]Listener) *BridgeWorker {
	return &BridgeWorker{
		ctx:        ctx,
		id:         id,
		workerChan: make(chan JobHandler, size),
		listeners:  listeners,
	}
}

func (w *BridgeWorker) String() string {
	return fmt.Sprintf("{ id: %d, currentSize: %d }", w.id, len(w.workerChan))
}

func (w *BridgeWorker) ProcessJob(job JobHandler) error {
	val, err := job.Process()
	if err != nil {
		log.Error("[BridgeWorker] failed while processing job", "id", job.GetID(), "err", err, "stack", stack.Trace().String())
		return err
	}
	if job.GetType() == ListenHandler && job.GetSubscriptionName() != "" {
		job.GetListener().SendCallbackJobs(w.listeners, job.GetSubscriptionName(), job.GetTransaction(), val)
	}
	return nil
}

func (w *BridgeWorker) Context() context.Context {
	return w.ctx
}

func (w *BridgeWorker) IsClose() bool {
	return atomic.LoadInt32(&w.isClose) != 0
}

func (w *BridgeWorker) Channel() chan JobHandler {
	return w.workerChan
}

func (w *BridgeWorker) Close() {
	atomic.StoreInt32(&w.isClose, 1)
	close(w.workerChan)
}
