package bridge_core

import (
	"context"
	"github.com/axieinfinity/bridge-core/adapters"
	"github.com/axieinfinity/bridge-core/metrics"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/log"
	"gorm.io/gorm"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultWorkers      = 8182
	defaultMaxQueueSize = 4096
	defaultBackoff      = 5
)

type Stats struct {
	RetryableQueue int
	Queue          int
	RetryingJob    int32
}

type Pool struct {
	ctx context.Context

	lock                sync.Mutex
	retryableWaitGroup  *sync.WaitGroup
	numberOfRetryingJob int32

	cfg     *Config
	Workers []Worker

	// message backoff
	MaxRetry int32
	BackOff  int32

	// Queue holds a list of worker
	Queue chan chan JobHandler

	// JobChan receives new job
	JobChan       chan JobHandler
	RetryJobChan  chan JobHandler
	FailedJobChan chan JobHandler

	jobId        int32
	MaxQueueSize int

	store    stores.MainStore
	stop     chan struct{}
	isClosed atomic.Value
	utils    utils.Utils
}

func NewPool(ctx context.Context, cfg *Config, db *gorm.DB, workers []Worker) *Pool {
	if cfg.NumberOfWorkers <= 0 {
		cfg.NumberOfWorkers = defaultWorkers
	}
	if cfg.MaxQueueSize <= 0 {
		cfg.MaxQueueSize = defaultMaxQueueSize
	}
	if cfg.BackOff <= 0 {
		cfg.BackOff = defaultBackoff
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = defaultMaxRetry
	}
	pool := &Pool{
		ctx:                ctx,
		cfg:                cfg,
		MaxRetry:           cfg.MaxRetry,
		BackOff:            cfg.BackOff,
		MaxQueueSize:       cfg.MaxQueueSize,
		store:              stores.NewMainStore(db),
		stop:               make(chan struct{}),
		isClosed:           atomic.Value{},
		utils:              utils.NewUtils(),
		retryableWaitGroup: &sync.WaitGroup{},
	}

	pool.isClosed.Store(false)
	pool.JobChan = make(chan JobHandler, pool.MaxQueueSize*cfg.NumberOfWorkers)
	pool.FailedJobChan = make(chan JobHandler, pool.MaxQueueSize)
	pool.RetryJobChan = make(chan JobHandler, pool.MaxQueueSize)
	pool.Queue = make(chan chan JobHandler, pool.MaxQueueSize)

	if workers != nil {
		if cfg.NumberOfWorkers < len(workers) {
			panic("number-of-workers reaches maximum allowance number")
		}
		pool.Workers = workers
	}

	if adapters.AppConfig.Prometheus.TurnOn {
		metrics.RunPusher(ctx)
	}
	return pool
}

func (p *Pool) AddWorkers(workers []Worker) {
	if workers == nil || p.cfg.NumberOfWorkers < len(workers) {
		panic("number-of-workers reaches maximum allowance number or empty")
	}
	p.Workers = workers
}

func (p *Pool) startWorker(w Worker) {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok && err.Error() == "send on closed channel" {
				w.Close()
				return
			}
			log.Error("[BridgeWorker][addToQueue] recover from panic", "message", r, "trace", string(debug.Stack()))
		}
	}()
	for {
		// push worker chan into queue if worker has not closed yet
		p.Queue <- w.Channel()
		select {
		case job := <-w.Channel():
			if job == nil {
				continue
			}
			if p.isClosed.Load().(bool) {
				// update job to db
				if err := job.Update(stores.STATUS_PENDING); err != nil {
					log.Error("[Pool] failed on updating failed job", "err", err, "jobType", job.GetType())
				}
				continue
			}
			log.Debug("processing job", "id", job.GetID(), "nextTry", job.GetNextTry(), "retryCount", job.GetRetryCount(), "type", job.GetType())
			if err := w.ProcessJob(job); err != nil {
				// update try and next retry time
				if job.GetRetryCount()+1 > job.GetMaxTry() {
					log.Info("[Pool][processJob] job reaches its maxTry", "jobTransaction", job.GetTransaction().GetHash().Hex())
					p.FailedJobChan <- job
					continue
				}
				job.IncreaseRetryCount()
				job.UpdateNextTry(time.Now().Unix() + int64(job.GetRetryCount()*job.GetBackOff()))
				// send to retry job chan
				p.RetryJob(job)
			}
		case <-w.Context().Done():
			w.Close()
			return
		}
	}
}

func (p *Pool) closedChannelRecover(cb func()) {
	if r := recover(); r != nil {
		if err, ok := r.(error); ok && err.Error() == "send on closed channel" {
			cb()
			return
		}
		log.Error("recover from panic", "message", r, "trace", string(debug.Stack()))
	}
}

func (p *Pool) RetryJob(job JobHandler) {
	defer p.closedChannelRecover(func() {
		p.updateRetryingJob(job)
	})
	if job == nil {
		return
	}
	for len(p.RetryJobChan) == p.cfg.MaxQueueSize {
		log.Info("[Pool] RetryJobChan is full...")
		time.Sleep(time.Second)
	}
	p.RetryJobChan <- job
}

func (p *Pool) Enqueue(job JobHandler) {
	defer p.closedChannelRecover(func() {
		p.updateRetryingJob(job)
	})
	if job == nil {
		return
	}
	for len(p.JobChan) == (p.cfg.MaxQueueSize * p.cfg.NumberOfWorkers) {
		log.Info("[Pool] JobChan is full...")
		time.Sleep(time.Second)
	}
	p.JobChan <- job
}

func (p *Pool) SendJobToWorker(workerCh chan JobHandler, job JobHandler) {
	defer p.closedChannelRecover(func() {
		p.updateRetryingJob(job)
	})
	if job == nil {
		return
	}
	workerCh <- job
}

func (p *Pool) Start(closeFunc func()) {
	if p.Workers == nil {
		panic("workers list is empty")
	}
	for _, worker := range p.Workers {
		go p.startWorker(worker)
	}
	for {
		select {
		case job := <-p.FailedJobChan:
			p.processFailedJob(job)
		case job := <-p.RetryJobChan:
			atomic.AddInt32(&p.numberOfRetryingJob, 1)
			p.retryableWaitGroup.Add(1)
			go p.PrepareRetryableJob(job)
		case job := <-p.JobChan:
			if job == nil {
				continue
			}
			if p.isClosed.Load().(bool) {
				if err := job.Update(stores.STATUS_PENDING); err != nil {
					log.Error("[Pool] failed on saving processing job", "err", err, "jobType", job.GetType())
				}
				continue
			}
			log.Debug("[Pool] jobChan received a job", "jobId", job.GetID(), "nextTry", job.GetNextTry(), "type", job.GetType())
			workerCh := <-p.Queue
			p.SendJobToWorker(workerCh, job)
		case <-p.ctx.Done():
			log.Info("closing pool...")
			p.isClosed.Store(true)

			// call close function firstly
			if closeFunc != nil {
				closeFunc()
			}

			// close all available channels to prevent further data send to pool's channels
			close(p.JobChan)
			close(p.FailedJobChan)
			close(p.RetryJobChan)
			close(p.Queue)

			for {
				log.Info("checking retrying jobs")
				job, more := <-p.RetryJobChan
				if !more {
					break
				}
				// update job
				p.updateRetryingJob(job)
			}

			for {
				log.Info("checking failedJobChan")
				job, more := <-p.FailedJobChan
				if !more {
					break
				}
				p.processFailedJob(job)
			}
			log.Info("finish closing pool")

			// wait for all on-fly retryable jobs are inserted to db
			p.retryableWaitGroup.Wait()

			// send signal to stop the program
			close(p.stop)
			return
		}
	}
}

func (p *Pool) Stats() Stats {
	return Stats{
		RetryableQueue: len(p.RetryJobChan),
		Queue:          len(p.JobChan),
		RetryingJob:    atomic.LoadInt32(&p.numberOfRetryingJob),
	}
}

func (p *Pool) PrepareRetryableJob(job JobHandler) {
	// if pool is closed, try update job to db
	if p.isClosed.Load().(bool) {
		log.Info("pool closed, update retrying job to database")
		p.updateRetryingJob(job)
	}
	dur := time.Until(time.Unix(job.GetNextTry(), 0))
	if dur <= 0 {
		return
	}

	defer func() {
		p.retryableWaitGroup.Done()
		atomic.AddInt32(&p.numberOfRetryingJob, -1)
	}()

	timer := time.NewTimer(dur)
	select {
	case <-timer.C:
		p.Enqueue(job)
	case <-p.ctx.Done():
		log.Info("pool closed, update retrying job to database")
		p.updateRetryingJob(job)
	}
}

func (p *Pool) updateRetryingJob(job JobHandler) {
	if job == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	if err := job.Update(stores.STATUS_PENDING); err != nil {
		log.Error("[Pool] failed on updating retrying job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		return
	}
}

// processFailedJob updates job's status to `failed` to database
func (p *Pool) processFailedJob(job JobHandler) {
	if job == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	if err := job.Update(stores.STATUS_FAILED); err != nil {
		log.Error("[Pool] failed on updating failed job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		return
	}
}

func (p *Pool) IsClosed() bool {
	return p.isClosed.Load().(bool)
}

func (p *Pool) Wait() {
	<-p.stop
}
