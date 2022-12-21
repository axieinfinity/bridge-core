package bridge_core

import (
	"context"
	"fmt"
	"github.com/axieinfinity/bridge-core/utils"
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
	PoolChannel() chan<- JobHandler
	FailedChannel() chan<- JobHandler
	WorkersQueue() chan chan JobHandler
}

type BridgeWorker struct {
	ctx context.Context

	utilWrapper utils.Utils

	id int

	// queue is passed from subscriber is used to add workerChan to queue
	queue chan chan JobHandler

	// mainChain is controller's jobChan which is used to push job back to controller
	mainChan chan<- JobHandler

	// workerChan is used to receive and process job
	workerChan chan JobHandler

	failedChan  chan<- JobHandler
	successChan chan<- JobHandler

	listeners map[string]Listener
	isClose   int32
}

func NewWorker(ctx context.Context, id int, mainChan, failedChan chan<- JobHandler, queue chan chan JobHandler, size int, listeners map[string]Listener) *BridgeWorker {
	return &BridgeWorker{
		ctx:         ctx,
		id:          id,
		workerChan:  make(chan JobHandler, size),
		mainChan:    mainChan,
		failedChan:  failedChan,
		queue:       queue,
		listeners:   listeners,
		utilWrapper: utils.NewUtils(),
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

func (w *BridgeWorker) WorkersQueue() chan chan JobHandler {
	return w.queue
}

func (w *BridgeWorker) PoolChannel() chan<- JobHandler {
	return w.mainChan
}

func (w *BridgeWorker) FailedChannel() chan<- JobHandler {
	return w.failedChan
}

func (w *BridgeWorker) Close() {
	atomic.StoreInt32(&w.isClose, 1)
	close(w.workerChan)
}
