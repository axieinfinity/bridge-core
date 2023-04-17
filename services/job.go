package services

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/zyedidia/generic/queue"
)

type JobService interface {
	Process(context.Context, types.Job)
	Start(context.Context) error
	Stop(context.Context) error
}

type jobService struct {
	pool types.Pool[types.Worker]

	store stores.MainStore

	jobChan       chan types.Job
	failedJobChan chan types.Job
	retryQueue    *queue.Queue[*retryJob]

	stopped atomic.Bool
}

type retryJob struct {
	at  int64
	job types.Job
}

func (s *jobService) Process(ctx context.Context, h types.Job) {
	if s.stopped.Load() {
		return
	}
	s.jobChan <- h
}

func (s *jobService) Start(ctx context.Context) error {
	var (
		retryTicker = time.NewTicker(time.Second)
	)
	for {
		select {
		case j := <-s.jobChan:
			w := s.pool.Get()
			err := w.ProcessJob(ctx, j)
			if err != nil {
				s.retryQueue.Enqueue(&retryJob{
					at:  j.NextTry(),
					job: j,
				})
			}

			s.pool.Put(w)
		case j := <-s.failedJobChan:
			j.Save(stores.STATUS_FAILED)
		case <-retryTicker.C:
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

func (s *jobService) Stop(ctx context.Context) error {
	s.stopped.Store(true)
	for !s.retryQueue.Empty() {
		rj := s.retryQueue.Dequeue()
		rj.job.Save(stores.STATUS_PENDING)
	}
	close(s.failedJobChan)
	return nil
}

func NewJobService(
	pool types.Pool[types.Worker],
	store stores.MainStore,
) JobService {
	return &jobService{
		pool:          pool,
		store:         store,
		retryQueue:    queue.New[*retryJob](),
		failedJobChan: make(chan types.Job),
	}
}
