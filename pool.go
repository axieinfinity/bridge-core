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
)

type Pool struct {
	ctx context.Context

	cfg     *Config
	Workers []Worker

	// message backoff
	MaxRetry int32
	BackOff  int32

	// Queue holds a list of worker
	Queue chan chan JobHandler

	// JobChan receives new job
	JobChan        chan JobHandler
	SuccessJobChan chan JobHandler
	FailedJobChan  chan JobHandler
	PrepareJobChan chan JobHandler

	jobId         int32
	processedJobs sync.Map

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
	pool := &Pool{
		ctx:          ctx,
		cfg:          cfg,
		MaxRetry:     100,
		BackOff:      5,
		MaxQueueSize: defaultMaxQueueSize,
		store:        stores.NewMainStore(db),
		stop:         make(chan struct{}),
		isClosed:     atomic.Value{},
		utils:        utils.NewUtils(),
	}

	pool.isClosed.Store(false)
	pool.JobChan = make(chan JobHandler, pool.MaxQueueSize*cfg.NumberOfWorkers)
	pool.PrepareJobChan = make(chan JobHandler, pool.MaxQueueSize)
	pool.SuccessJobChan = make(chan JobHandler, pool.MaxQueueSize)
	pool.FailedJobChan = make(chan JobHandler, pool.MaxQueueSize)
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
			log.Error("[BridgeWorker][addToQueue] recover from panic", "message", r, "trace", string(debug.Stack()))
		}
	}()
	for {
		// push worker chan into queue if worker has not closed yet
		if !w.IsClose() {
			w.WorkersQueue() <- w.Channel()
		}
		select {
		case job := <-w.Channel():
			log.Info("processing job", "id", job.GetID(), "nextTry", job.GetNextTry(), "retryCount", job.GetRetryCount(), "type", job.GetType())
			if job.GetNextTry() == 0 || job.GetNextTry() <= time.Now().Unix() {
				metrics.Pusher.IncrGauge(metrics.ProcessingJobMetric, 1)
				w.ProcessJob(job)
				metrics.Pusher.IncrGauge(metrics.ProcessingJobMetric, -1)
				continue
			}
			// push the job back to mainChan
			w.PoolChannel() <- job
		case <-w.Context().Done():
			w.Close()
			return
		}
	}
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
		case job := <-p.SuccessJobChan:
			p.processSuccessJob(job)
		case job := <-p.FailedJobChan:
			p.processFailedJob(job)
		case job := <-p.PrepareJobChan:
			if job == nil {
				continue
			}
			// add new job to database before processing
			if err := p.PrepareJob(job); err != nil {
				log.Error("[Pool] failed on preparing job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
				metrics.Pusher.IncrCounter(metrics.PreparingFailedJobMetric, 1)
				continue
			}
			metrics.Pusher.IncrCounter(metrics.PreparingSuccessJobMetric, 1)
			p.JobChan <- job
		case job := <-p.JobChan:
			if job == nil {
				continue
			}
			// get 1 workerCh from queue and push job to this channel
			hash := job.Hash()
			if _, ok := p.processedJobs.LoadOrStore(hash, struct{}{}); ok {
				continue
			}
			log.Info("[Pool] jobChan received a job", "jobId", job.GetID(), "nextTry", job.GetNextTry(), "type", job.GetType())
			workerCh := <-p.Queue
			workerCh <- job
		case <-p.ctx.Done():
			// prevent ctx.Done is called multiple times among routines.
			if p.isClosed.Load().(bool) {
				return
			} else {
				p.isClosed.Store(true)
			}

			// call close function firstly
			if closeFunc != nil {
				closeFunc()
			}

			// loop through prepare job chan to store all jobs to db
			for {
				if len(p.PrepareJobChan) == 0 {
					break
				}
				job, more := <-p.PrepareJobChan
				if !more {
					break
				}
				if err := p.PrepareJob(job); err != nil {
					log.Error("[Pool] error while storing all jobs from prepareJobChan to database in closing step", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
				}
			}

			// update all success jobs
			for {
				log.Info("checking successJobChan")
				if len(p.SuccessJobChan) == 0 {
					break
				}
				job, more := <-p.SuccessJobChan
				if !more {
					break
				}
				p.processSuccessJob(job)
			}

			// wait until all failed jobs are handled
			for {
				if len(p.FailedJobChan) == 0 {
					break
				}
				log.Info("checking failedJobChan")
				job, more := <-p.FailedJobChan
				if !more {
					break
				}
				p.processFailedJob(job)
			}

			// close all available channels
			close(p.PrepareJobChan)
			close(p.JobChan)
			close(p.SuccessJobChan)
			close(p.FailedJobChan)
			close(p.Queue)

			// send signal to stop the program
			p.stop <- struct{}{}
			break
		}
	}
}

// PrepareJob saves new job to database
func (p *Pool) PrepareJob(job JobHandler) error {
	if job == nil {
		return nil
	}
	log.Info("[PrepareJobChan] preparing new job", "job", job.String())
	// deduplication: get hash from data and type and check if it exists in `processedJobs` or not.
	hash := p.utils.RlpHash(struct {
		Data []byte
		Type int
	}{
		Data: job.GetData(),
		Type: job.GetType(),
	})
	if _, ok := p.processedJobs.Load(hash); ok {
		return nil
	}
	// save job to db if id = 0
	if job.GetID() == 0 {
		return job.Save()
	}
	// cache above hash to `processedJobs`
	p.processedJobs.Store(hash, struct{}{})
	return nil
}

// processSuccessJob updates job's status to `done` to database
func (p *Pool) processSuccessJob(job JobHandler) {
	if job == nil {
		return
	}

	log.Info("process job success", "id", job.GetID())
	if err := job.Update(stores.STATUS_DONE); err != nil {
		log.Error("[Pool] failed on updating success job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		// send back job to successJobChan
		p.SuccessJobChan <- job
		return
	}
}

// processFailedJob updates job's status to `failed` to database
func (p *Pool) processFailedJob(job JobHandler) {
	if job == nil {
		return
	}

	log.Info("process job failed", "id", job.GetID())
	if err := job.Update(stores.STATUS_FAILED); err != nil {
		log.Error("[Pool] failed on updating failed job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		// send back job to failedJobChan
		p.FailedJobChan <- job
		return
	}
}

func (p *Pool) IsClosed() bool {
	return p.isClosed.Load().(bool)
}

func (p *Pool) Wait() {
	<-p.stop
}
