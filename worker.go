package bridge_core

import (
	"context"
	"fmt"
	"github.com/axieinfinity/bridge-core/metrics"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/log"
	"sync/atomic"
	"time"
)

type Worker interface {
	Context() context.Context
	Close()
	ProcessJob(job JobHandler)
	IsClose() bool
	Channel() chan JobHandler
	PoolChannel() chan<- JobHandler
	FailedChannel() chan<- JobHandler
	SuccessChannel() chan<- JobHandler
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

func NewWorker(ctx context.Context, id int, mainChan, failedChan, successChan chan<- JobHandler, queue chan chan JobHandler, size int, listeners map[string]Listener) *BridgeWorker {
	return &BridgeWorker{
		ctx:         ctx,
		id:          id,
		workerChan:  make(chan JobHandler, size),
		mainChan:    mainChan,
		failedChan:  failedChan,
		successChan: successChan,
		queue:       queue,
		listeners:   listeners,
		utilWrapper: utils.NewUtils(),
	}
}

func (w *BridgeWorker) String() string {
	return fmt.Sprintf("{ id: %d, currentSize: %d }", w.id, len(w.workerChan))
}

func (w *BridgeWorker) ProcessJob(job JobHandler) {
	var (
		err error
		val []byte
	)

	val, err = job.Process()
	if err != nil {
		log.Error("[BridgeWorker] failed while processing job", "id", job.GetID(), "err", err)
		goto ERROR
	}
	if job.GetType() == ListenHandler && job.GetSubscriptionName() != "" {
		job.GetListener().SendCallbackJobs(w.listeners, job.GetSubscriptionName(), job.GetTransaction(), val)
	}
	metrics.Pusher.IncrCounter(metrics.ProcessedSuccessJobMetric, 1)
	w.successChan <- job
	return
ERROR:
	metrics.Pusher.IncrCounter(metrics.ProcessedFailedJobMetric, 1)

	if job.GetRetryCount()+1 > job.GetMaxTry() {
		log.Info("[BridgeWorker][processJob] job reaches its maxTry", "jobTransaction", job.GetTransaction().GetHash().Hex())
		w.failedChan <- job
		return
	}
	job.IncreaseRetryCount()
	job.UpdateNextTry(time.Now().Unix() + int64(job.GetRetryCount()*job.GetBackOff()))
	// push the job back to mainChan
	w.mainChan <- job
	log.Info("[BridgeWorker][processJob] job failed, added back to jobChan", "id", job.GetID(), "retryCount", job.GetRetryCount(), "nextTry", job.GetNextTry())
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

func (w *BridgeWorker) SuccessChannel() chan<- JobHandler {
	return w.successChan
}

func (w *BridgeWorker) Close() {
	atomic.StoreInt32(&w.isClose, 1)
	close(w.workerChan)
}
