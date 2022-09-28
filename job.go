package internal

import (
	"context"
	"fmt"
	"math/big"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/axieinfinity/bridge-core/metrics"
	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type Job struct {
	ID         int32
	Type       int
	Message    []interface{}
	RetryCount int32
	NextTry    int32
	MaxTry     int32
	BackOff    int32
	Listener   Listener
}

func (job Job) Hash() common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("j-%d-%d-%d", job.ID, job.RetryCount, job.NextTry)))
}

type Worker struct {
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

func NewWorker(ctx context.Context, id int, mainChan, failedChan, successChan chan<- JobHandler, queue chan chan JobHandler, size int, listeners map[string]Listener) *Worker {
	return &Worker{
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

func (w *Worker) String() string {
	return fmt.Sprintf("{ id: %d, currentSize: %d }", w.id, len(w.workerChan))
}

func (w *Worker) start() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("[Worker][addToQueue] recover from panic", "message", r, "trace", string(debug.Stack()))
		}
	}()
	for {
		// push worker chan into queue if worker has not closed yet
		if atomic.LoadInt32(&w.isClose) == 0 {
			w.queue <- w.workerChan
		}
		select {
		case job := <-w.workerChan:
			log.Info("processing job", "id", job.GetID(), "nextTry", job.GetNextTry(), "retryCount", job.GetRetryCount(), "type", job.GetType())
			if job.GetNextTry() == 0 || job.GetNextTry() <= time.Now().Unix() {
				metrics.Pusher.IncrGauge(metrics.ProcessingJobMetric, 1)
				w.processJob(job)
				metrics.Pusher.IncrGauge(metrics.ProcessingJobMetric, -1)
				continue
			}
			// push the job back to mainChan
			w.mainChan <- job
		case <-w.ctx.Done():
			atomic.StoreInt32(&w.isClose, 1)
			close(w.workerChan)
			return
		}
	}
}

func (w *Worker) processJob(job JobHandler) {
	var (
		err error
		val []byte
	)

	val, err = job.Process()
	if err != nil {
		log.Error("[Worker] failed while processing job", "id", job.GetID(), "err", err)
		goto ERROR
	}
	if job.GetType() == ListenHandler {
		job.GetListener().SendCallbackJobs(w.listeners, job.GetSubscriptionName(), job.GetTransaction(), val)
	}
	metrics.Pusher.IncrCounter(metrics.ProcessedSuccessJobMetric, 1)
	w.successChan <- job
	return
ERROR:
	metrics.Pusher.IncrCounter(metrics.ProcessedFailedJobMetric, 1)

	if job.GetRetryCount()+1 > job.GetMaxTry() {
		log.Info("[Worker][processJob] job reaches its maxTry", "jobTransaction", job.GetTransaction().GetHash().Hex())
		w.failedChan <- job
		return
	}
	job.IncreaseRetryCount()
	job.UpdateNextTry(time.Now().Unix() + int64(job.GetRetryCount()*job.GetBackOff()))
	// push the job back to mainChan
	w.mainChan <- job
	log.Info("[Worker][processJob] job failed, added back to jobChan", "id", job.GetID(), "retryCount", job.GetRetryCount(), "nextTry", job.GetNextTry())
}

type BaseJob struct {
	utilsWrapper utils.Utils

	id      int32
	jobType int

	retryCount int
	maxTry     int
	nextTry    int64
	backOff    int

	data []byte
	tx   Transaction

	subscriptionName string
	listener         Listener

	fromChainID *big.Int
	createdAt   time.Time
}

func NewBaseJob(listener Listener, job *models.Job, transaction Transaction) (*BaseJob, error) {
	chainId, err := hexutil.DecodeBig(job.FromChainId)
	if err != nil {
		return nil, err
	}
	return &BaseJob{
		jobType:          job.Type,
		retryCount:       job.RetryCount,
		maxTry:           20,
		nextTry:          time.Now().Unix() + int64(job.RetryCount*5),
		backOff:          5,
		data:             common.Hex2Bytes(job.Data),
		tx:               transaction,
		subscriptionName: job.SubscriptionName,
		listener:         listener,
		utilsWrapper:     utils.NewUtils(),
		fromChainID:      chainId,
		id:               int32(job.ID),
		createdAt:        time.Unix(job.CreatedAt, 0),
	}, nil
}

func (e *BaseJob) FromChainID() *big.Int {
	return e.fromChainID
}

func (e *BaseJob) GetID() int32 {
	return e.id
}

func (e *BaseJob) GetType() int {
	return e.jobType
}

func (e *BaseJob) GetRetryCount() int {
	return e.retryCount
}

func (e *BaseJob) GetNextTry() int64 {
	return e.nextTry
}

func (e *BaseJob) GetMaxTry() int {
	return e.maxTry
}

func (e *BaseJob) GetData() []byte {
	return e.data
}

func (e *BaseJob) GetValue() *big.Int {
	return e.tx.GetValue()
}

func (e *BaseJob) GetBackOff() int {
	return e.backOff
}

func (e *BaseJob) Process() ([]byte, error) {
	// save event data to database
	if e.jobType == ListenHandler {
		subscription, ok := e.listener.GetSubscriptions()[e.subscriptionName]
		if ok {
			if err := e.listener.GetStore().GetEventStore().Save(&models.Event{
				EventName:       subscription.Handler.Name,
				TransactionHash: e.tx.GetHash().Hex(),
				FromChainId:     hexutil.EncodeBig(e.FromChainID()),
				CreatedAt:       time.Now().Unix(),
			}); err != nil {
				log.Error(fmt.Sprintf("[%sListenJob][Process] error while storing event to database", e.listener.GetName()), "err", err)
			}
		}
		return e.data, nil
	}
	return nil, nil
}

func (e *BaseJob) Hash() common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("j-%d-%d-%d", e.id, e.retryCount, e.nextTry)))
}

func (e *BaseJob) IncreaseRetryCount() {
	e.retryCount++
}
func (e *BaseJob) UpdateNextTry(nextTry int64) {
	e.nextTry = nextTry
}

func (e *BaseJob) GetListener() Listener {
	return e.listener
}

func (e *BaseJob) GetSubscriptionName() string {
	return e.subscriptionName
}

func (e *BaseJob) GetTransaction() Transaction {
	return e.tx
}

func (e *BaseJob) Utils() utils.Utils {
	return e.utilsWrapper
}

func (e *BaseJob) Save() error {
	job := &models.Job{
		Listener:         e.listener.GetName(),
		SubscriptionName: e.subscriptionName,
		Type:             e.jobType,
		RetryCount:       e.retryCount,
		Status:           stores.STATUS_PENDING,
		Data:             common.Bytes2Hex(e.data),
		Transaction:      e.tx.GetHash().Hex(),
		CreatedAt:        time.Now().Unix(),
		FromChainId:      hexutil.EncodeBig(e.fromChainID),
	}
	if err := e.listener.GetStore().GetJobStore().Save(job); err != nil {
		return err
	}
	e.id = int32(job.ID)
	e.createdAt = time.Unix(job.CreatedAt, 0)
	return nil
}

func (e *BaseJob) Update(status string) error {
	job := &models.Job{
		ID:               int(e.id),
		Listener:         e.listener.GetName(),
		SubscriptionName: e.subscriptionName,
		Type:             e.jobType,
		RetryCount:       e.retryCount,
		Status:           status,
		Data:             common.Bytes2Hex(e.data),
		Transaction:      e.tx.GetHash().Hex(),
		CreatedAt:        time.Now().Unix(),
		FromChainId:      hexutil.EncodeBig(e.fromChainID),
	}
	if err := e.listener.GetStore().GetJobStore().Update(job); err != nil {
		return err
	}
	return nil
}

func (e *BaseJob) SetID(id int32) {
	e.id = id
}

func (e *BaseJob) CreatedAt() time.Time {
	return e.createdAt
}
