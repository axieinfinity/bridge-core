package orchestrators

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/zyedidia/generic/queue"
)

type TransactionOrchestrator interface {
	Process(context.Context, ...types.Job[types.Transaction])
	Start(context.Context) error
	Stop(context.Context) error
	Stats() *Stats
}

type transactionOrchestrator struct {
	pool  types.Pool[types.Worker[types.Transaction]]
	store stores.MainStore

	jobChan       chan types.Job[types.Transaction]
	failedJobChan chan types.Job[types.Transaction]
	retryQueue    *queue.Queue[*retryJob[types.Transaction]]

	stopped   atomic.Bool
	processed int64 // for debugging purpose
}

func (s *transactionOrchestrator) Process(ctx context.Context, jobs ...types.Job[types.Transaction]) {
	if s.stopped.Load() {
		return
	}
	for _, v := range jobs {
		s.jobChan <- v
	}
}

func (s *transactionOrchestrator) Start(ctx context.Context) error {
	var (
		retryTicker = time.NewTicker(time.Second)
	)
	for {
		select {
		case j := <-s.jobChan:
			// store job
			if err := s.store.GetEventStore().Save(&models.Event{
				EventName:       j.Event,
				TransactionHash: j.Data.GetHash().Hex(),
				FromChainId:     j.Data.GetChainID().String(),
				CreatedAt:       time.Now().Unix(),
			}); err != nil {
				log.Error(fmt.Sprintf("[%sListenJob][Process] error while storing event to database", j.Name), "err", err)
			}

			n := atomic.AddInt64(&s.processed, 1)
			log.Info("processing job: ", "n", n)
			w := s.pool.Get()
			err := w.ProcessJob(ctx, j)
			if err != nil {
				j.RetryCount++
				s.retryQueue.Enqueue(&retryJob[types.Transaction]{
					at:  j.At,
					job: j,
				})
			}

			s.pool.Put(w)
		case j := <-s.failedJobChan:
			if err := s.store.GetJobStore().Save(&models.Job{
				ID:               j.ID,
				Listener:         j.Name,
				SubscriptionName: j.Event,
				Status:           stores.STATUS_FAILED,
				RetryCount:       j.RetryCount,
				Type:             j.Type,
			}); err != nil {
				log.Error("save failed job got error", "err", err)
			}
		case <-retryTicker.C:
			log.Info("Hi mom im from job orchestrator")

			now := time.Now().Unix()
			for !s.retryQueue.Empty() && s.retryQueue.Peek().at <= now {
				rj := s.retryQueue.Dequeue()
				s.Process(ctx, rj.job)
			}
		case <-ctx.Done():
			log.Info("Closing transaction service...")
			return nil
		}
	}
}

func (s *transactionOrchestrator) Stop(ctx context.Context) error {
	log.Info("stop transaction orchestrator")

	return nil
}

func (s *transactionOrchestrator) Stats() *Stats {
	return &Stats{}
}

func NewTransactionOrchestrator(
	pool types.Pool[types.Worker[types.Transaction]],
	store stores.MainStore,
) TransactionOrchestrator {
	return &transactionOrchestrator{
		pool:          pool,
		store:         store,
		retryQueue:    queue.New[*retryJob[types.Transaction]](),
		jobChan:       make(chan types.Job[types.Transaction]),
		failedJobChan: make(chan types.Job[types.Transaction]),
	}
}
