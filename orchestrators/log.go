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

type Stats struct {
	RetryableQueue int
	Queue          int
	RetryingJob    int32
}

func (a *Stats) Merge(b *Stats) *Stats {
	return &Stats{
		RetryableQueue: a.RetryableQueue + b.RetryableQueue,
		Queue:          a.Queue + b.Queue,
		RetryingJob:    a.RetryingJob + b.RetryingJob,
	}
}

type LogOrchestrator interface {
	Process(context.Context, ...types.Job[types.Log])
	Start(context.Context) error
	Stop(context.Context) error
	Stats() *Stats
}

type jobOrchestrator struct {
	pool types.Pool[types.Worker[types.Log]]

	store stores.MainStore

	jobChan       chan types.Job[types.Log]
	failedJobChan chan types.Job[types.Log]
	retryQueue    *queue.Queue[*retryJob[types.Log]]

	stopped atomic.Bool

	processed int64 // for debugging purpose
}

type retryJob[T any] struct {
	at  int64
	job types.Job[T]
}

func (s *jobOrchestrator) Process(ctx context.Context, h ...types.Job[types.Log]) {
	if s.stopped.Load() {
		return
	}
	for _, v := range h {
		s.jobChan <- v
	}
}

func (s *jobOrchestrator) Start(ctx context.Context) error {
	var (
		retryTicker = time.NewTicker(time.Second)
	)
	for {
		select {
		case j := <-s.jobChan:
			// store job
			if err := s.store.GetEventStore().Save(&models.Event{
				EventName:       j.Event,
				TransactionHash: j.Data.GetTransactionHash(),
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
				s.retryQueue.Enqueue(&retryJob[types.Log]{
					at:  j.At,
					job: j,
				})
			}

			s.pool.Put(w)
		case j := <-s.failedJobChan:
			s.store.GetJobStore().Save(&models.Job{
				ID:               j.ID,
				Listener:         j.Name,
				SubscriptionName: j.Event,
				Status:           stores.STATUS_FAILED,
				RetryCount:       j.RetryCount,
				Type:             j.Type,
			})
			// j.Save(stores.STATUS_FAILED)
		case <-retryTicker.C:
			log.Info("Hi mom im from log orchestrator")

			now := time.Now().Unix()
			for !s.retryQueue.Empty() && s.retryQueue.Peek().at <= now {
				rj := s.retryQueue.Dequeue()
				s.Process(ctx, rj.job)
			}
		case <-ctx.Done():
			log.Info("Closing job service...")
			return nil
		}
	}
}

func (s *jobOrchestrator) Stop(ctx context.Context) error {
	log.Info("stop log orchestrator")
	s.stopped.Store(true)
	for !s.retryQueue.Empty() {
		rj := s.retryQueue.Dequeue()
		if err := s.store.GetJobStore().Save(&models.Job{
			ID:               rj.job.ID,
			Listener:         rj.job.Name,
			SubscriptionName: rj.job.Event,
			Status:           stores.STATUS_FAILED,
			RetryCount:       rj.job.RetryCount,
			Type:             rj.job.Type,
		}); err != nil {
			log.Error("store job to db got error", "err", err)
		}
	}
	close(s.failedJobChan)
	return nil
}

func (s *jobOrchestrator) Stats() *Stats {
	return &Stats{}
}

func NewLogService(
	pool types.Pool[types.Worker[types.Log]],
	store stores.MainStore,
) LogOrchestrator {
	return &jobOrchestrator{
		pool:          pool,
		store:         store,
		retryQueue:    queue.New[*retryJob[types.Log]](),
		failedJobChan: make(chan types.Job[types.Log]),
		jobChan:       make(chan types.Job[types.Log]),
	}
}
