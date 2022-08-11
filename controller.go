package internal

import (
	"context"
	"errors"
	"github.com/axieinfinity/bridge-contracts"
	"github.com/axieinfinity/bridge-core/metrics"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"gorm.io/gorm"
)

const (
	defaultBatchSize        = 100
	defaultWorkers          = 8182
	defaultMaxQueueSize     = 4096
	defaultCoolDownDuration = 1
	defaultMaxRetry         = 10
	defaultTaskInterval     = 3
)

var listeners map[string]func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener

func init() {
	listeners = make(map[string]func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener)
}

func AddListener(name string, initFunc func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener) {
	listeners[name] = initFunc
}

type Controller struct {
	lock       sync.Mutex
	ctx        context.Context
	cancelFunc context.CancelFunc

	listeners   map[string]Listener
	HandlerABIs map[string]*abi.ABI
	utilWrapper utils.Utils

	Workers []*Worker

	// message backoff
	MaxRetry int32
	BackOff  int32

	// coolDownDuration is used to sleep for a while when a channel reaches its size
	coolDownDuration int

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
	cfg          *Config

	store               stores.MainStore
	stop                chan struct{}
	isClosed            atomic.Value
	hasSubscriptionType map[string]map[int]bool
}

func New(cfg *Config, db *gorm.DB, helpers utils.Utils) (*Controller, error) {
	if cfg.NumberOfWorkers <= 0 {
		cfg.NumberOfWorkers = defaultWorkers
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Controller{
		cfg:                 cfg,
		ctx:                 ctx,
		cancelFunc:          cancel,
		listeners:           make(map[string]Listener),
		HandlerABIs:         make(map[string]*abi.ABI),
		utilWrapper:         utils.NewUtils(),
		Workers:             make([]*Worker, 0),
		MaxRetry:            100,
		BackOff:             5,
		MaxQueueSize:        defaultMaxQueueSize,
		coolDownDuration:    defaultCoolDownDuration,
		store:               stores.NewMainStore(db),
		stop:                make(chan struct{}),
		isClosed:            atomic.Value{},
		hasSubscriptionType: make(map[string]map[int]bool),
	}

	metrics.RunPusher(ctx)

	c.isClosed.Store(false)
	if helpers != nil {
		c.utilWrapper = helpers
	}
	c.JobChan = make(chan JobHandler, c.MaxQueueSize*cfg.NumberOfWorkers)
	c.PrepareJobChan = make(chan JobHandler, c.MaxQueueSize)
	c.SuccessJobChan = make(chan JobHandler, c.MaxQueueSize)
	c.FailedJobChan = make(chan JobHandler, c.MaxQueueSize)
	c.Queue = make(chan chan JobHandler, c.MaxQueueSize)

	// add listeners from config
	for name, lsConfig := range c.cfg.Listeners {
		if lsConfig.LoadInterval <= 0 {
			lsConfig.LoadInterval = defaultTaskInterval
		}
		lsConfig.LoadInterval *= time.Second
		lsConfig.Name = name

		// load abi from lsConfig
		if err := c.LoadABIsFromConfig(lsConfig); err != nil {
			return nil, err
		}

		// Invoke init function which is based on listener's name
		initFunc, ok := listeners[name]
		if !ok {
			continue
		}
		l := initFunc(c.ctx, lsConfig, c.store, c.utilWrapper)
		if l == nil {
			return nil, errors.New("listener is nil")
		}
		// set prepare job chan to listener
		l.SetPrepareJobChan(c.PrepareJobChan)

		// add listener to controller
		c.listeners[name] = l
		c.hasSubscriptionType[name] = make(map[int]bool)

		if lsConfig.GetLogsBatchSize == 0 {
			lsConfig.GetLogsBatchSize = defaultBatchSize
		}

		// filtering subscription, get all subscriptionType available for each listener
		for _, subscription := range l.GetSubscriptions() {
			if c.hasSubscriptionType[name][subscription.Type] {
				continue
			}
			c.hasSubscriptionType[name][subscription.Type] = true
		}

	}

	// init workers
	for i := 0; i < cfg.NumberOfWorkers; i++ {
		w := NewWorker(ctx, i, c.PrepareJobChan, c.FailedJobChan, c.SuccessJobChan, c.Queue, c.MaxQueueSize, c.listeners)
		c.Workers = append(c.Workers, w)
	}

	return c, nil
}

// LoadABIsFromConfig loads all ABIPath and add results to Handler.ABI
func (c *Controller) LoadABIsFromConfig(lsConfig *LsConfig) (err error) {
	for _, subscription := range lsConfig.Subscriptions {
		if subscription.Handler.Contract == "" {
			continue
		}
		// load abi for handler
		if subscription.Handler.ABI, err = bridge_contracts.ABIMaps[subscription.Handler.Contract].GetAbi(); err != nil {
			return err
		}
	}
	return
}

// prepareJob saves new job to database
func (c *Controller) prepareJob(job JobHandler) error {
	if job == nil {
		return nil
	}
	if job.GetID() == 0 {
		return job.Save()
	}
	return nil
}

// processSuccessJob updates job's status to `done` to database
func (c *Controller) processSuccessJob(job JobHandler) {
	if job == nil {
		return
	}

	log.Info("process job success", "id", job.GetID())
	if err := job.Update(stores.STATUS_DONE); err != nil {
		log.Error("[Controller] failed on updating success job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		// send back job to successJobChan
		c.SuccessJobChan <- job
		return
	}
}

// processFailedJob updates job's status to `failed` to database
func (c *Controller) processFailedJob(job JobHandler) {
	if job == nil {
		return
	}

	log.Info("process job failed", "id", job.GetID())
	if err := job.Update(stores.STATUS_FAILED); err != nil {
		log.Error("[Controller] failed on updating failed job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
		// send back job to failedJobChan
		c.FailedJobChan <- job
		return
	}
}

func (c *Controller) Start() error {
	for _, worker := range c.Workers {
		go worker.start()
	}
	go func() {
		for {
			select {
			case job := <-c.SuccessJobChan:
				c.processSuccessJob(job)
			case job := <-c.FailedJobChan:
				c.processFailedJob(job)
			case job := <-c.PrepareJobChan:
				// add new job to database before processing
				if err := c.prepareJob(job); err != nil {
					log.Error("[Controller] failed on preparing job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
					metrics.Pusher.IncrCounter(metrics.PreparingFailedJobMetric, 1)
					continue
				}
				metrics.Pusher.IncrCounter(metrics.PreparingSuccessJobMetric, 1)
				c.JobChan <- job
			case job := <-c.JobChan:
				if job == nil {
					continue
				}
				// get 1 workerCh from queue and push job to this channel
				hash := job.Hash()
				if _, ok := c.processedJobs.Load(hash); ok {
					continue
				}
				c.processedJobs.Store(hash, struct{}{})
				log.Info("[Controller] jobChan received a job", "jobId", job.GetID(), "nextTry", job.GetNextTry(), "type", job.GetType())
				workerCh := <-c.Queue
				workerCh <- job
			case <-c.ctx.Done():
				// prevent ctx.Done is called multiple times among routines.
				if c.isClosed.Load().(bool) {
					return
				} else {
					c.isClosed.Store(true)
				}

				// close listeners first to prevent further tasks processing
				c.closeListeners()

				// loop through prepare job chan to store all jobs to db
				for {
					if len(c.PrepareJobChan) == 0 {
						break
					}
					job, more := <-c.PrepareJobChan
					if !more {
						break
					}
					if err := c.prepareJob(job); err != nil {
						log.Error("[Controller] error while storing all jobs from prepareJobChan to database in closing step", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
					}
				}

				// update all success jobs
				for {
					log.Info("checking successJobChan")
					if len(c.SuccessJobChan) == 0 {
						break
					}
					job, more := <-c.SuccessJobChan
					if !more {
						break
					}
					c.processSuccessJob(job)
				}

				// wait until all failed jobs are handled
				for {
					if len(c.FailedJobChan) == 0 {
						break
					}
					log.Info("checking failedJobChan")
					job, more := <-c.FailedJobChan
					if !more {
						break
					}
					c.processFailedJob(job)
				}

				// close all available channels
				close(c.PrepareJobChan)
				close(c.JobChan)
				close(c.SuccessJobChan)
				close(c.FailedJobChan)
				close(c.Queue)

				// send signal to stop the program
				c.stop <- struct{}{}
				break
			}
		}
	}()
	c.processPendingJobs()
	c.startListeners()
	return nil
}

func (c *Controller) processPendingJobs() {
	// load all pending jobs from database
	jobs, err := c.store.GetJobStore().GetPendingJobs()
	if err != nil {
		// just log and do nothing.
		log.Error("[Controller] error while getting pending jobs from database", "err", err)
	}
	for _, job := range jobs {
		listener, ok := c.listeners[job.Listener]
		if !ok || listener.IsDisabled() {
			continue
		}
		if job.Type == CallbackHandler && job.Method == "" {
			// invalid job, update it to failed
			job.Status = stores.STATUS_FAILED
			if err = listener.GetStore().GetJobStore().Update(job); err != nil {
				log.Error("[Controller] error while updating invalid job", "err", err, "id", job.ID)
			}
			continue
		}
		j, err := listener.NewJobFromDB(job)
		if err != nil {
			log.Error("[Controller] error while init job from db", "err", err, "jobId", job.ID, "type", job.Type)
			continue
		}
		// add job to jobChan
		if j != nil {
			c.JobChan <- j
		}
	}
}

func (c *Controller) startListeners() {
	// make sure all listeners are up-to-date
	for _, listener := range c.listeners {
		if listener.IsDisabled() {
			continue
		}
		for {
			if listener.IsUpTodate() {
				break
			}
			// sleep for 10s
			time.Sleep(10 * time.Second)
		}
	}
	// run all events listeners
	for _, listener := range c.listeners {
		if listener.IsDisabled() {
			continue
		}
		go listener.Start()
		go c.startListening(listener, 0)
	}
}

func (c *Controller) closeListeners() {
	for _, listener := range c.listeners {
		listener.Close()
	}
}

func (c *Controller) Wait() {
	<-c.stop
}

// startListener starts listening events for a listener, it comes with a tryCount which close this listener if tryCount reaches 10 times
func (c *Controller) startListening(listener Listener, tryCount int) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("[Controller][startListener] recover from panic", "message", r)
		}
	}()
	// panic when tryCount reaches 10 times panic
	if tryCount >= defaultMaxRetry {
		log.Error("[Controller][startListener] maximum try has been reached, close listener", "listener", listener.GetName())
		listener.Close()
		return
	}

	// check if listener is behind or not
	latestBlockHeight, err := listener.GetLatestBlockHeight()
	if err != nil {
		log.Error("[Controller][startListener] error while get latest block", "err", err, "listener", listener.GetName())
		// otherwise retry startListener
		time.Sleep(time.Duration(tryCount+1) * time.Second)
		go c.startListening(listener, tryCount+1)
		return
	}
	// reset fromHeight if it is out of allowed blocks range
	if listener.Config().ProcessWithinBlocks > 0 && latestBlockHeight-listener.GetInitHeight() > listener.Config().ProcessWithinBlocks {
		listener.SetInitHeight(latestBlockHeight - listener.Config().ProcessWithinBlocks)
	}
	log.Info("[Controller] Latest Block", "height", latestBlockHeight, "listener", listener.GetName())
	// start processing past blocks
	currentBlock := listener.GetCurrentBlock()
	if currentBlock != nil {
		if err := c.processBehindBlock(listener, currentBlock.GetHeight(), latestBlockHeight); err != nil {
			log.Error("[Controller][startListener] error while processing behind block", "err", err, "height", currentBlock.GetHeight(), "latestBlockHeight", latestBlockHeight)
			time.Sleep(time.Duration(tryCount+1) * time.Second)
			go c.startListening(listener, tryCount+1)
		}
	}
	// start listening to block's events
	tick := time.NewTicker(listener.Period())
	for {
		select {
		case <-listener.Context().Done():
			return
		case <-tick.C:
			// stop if controller is closed
			if c.isClosed.Load().(bool) {
				return
			}
			latest, err := listener.GetLatestBlockHeight()
			if err != nil {
				log.Error("[Controller][Watcher] error while get latest block height", "err", err)
				continue
			}
			currentBlock = listener.GetCurrentBlock()
			// do nothing if currentBlock is within safe block range
			if currentBlock.GetHeight() > latest-listener.GetSafeBlockRange() {
				continue
			}
			// if current block is behind safeBlockRange then process without waiting
			if err := c.processBehindBlock(listener, currentBlock.GetHeight(), latest); err != nil {
				log.Error("[Controller][Watcher] error while processing behind block", "err", err, "height", currentBlock.GetHeight(), "latestBlockHeight", latestBlockHeight)
				continue
			} else {
				currentBlock = listener.GetCurrentBlock()
			}
		}
	}
}

func (c *Controller) processBehindBlock(listener Listener, height, latestBlockHeight uint64) error {
	if latestBlockHeight-listener.GetSafeBlockRange() > height {
		var (
			safeBlock, block  Block
			tryCount          int
			err               error
			processedToHeight uint64
		)
		safeBlock, err = listener.GetBlock(latestBlockHeight - listener.GetSafeBlockRange())
		if err != nil {
			log.Error("[Controller][Process] error while getting safeBlock", "err", err, "latest", latestBlockHeight)
			return err
		}
		// process logs
		if c.hasSubscriptionType[listener.GetName()][LogEvent] {
			processedToHeight = c.processBatchLogs(listener, height, safeBlock.GetHeight())
		}
		// process transactions
		if c.hasSubscriptionType[listener.GetName()][TxEvent] {
			for height <= processedToHeight {
				block, err = listener.GetBlock(height)
				if err != nil {
					log.Error("[Controller][processBlock] error while get block", "err", err, "listener", listener.GetName(), "height", height)
					tryCount++
					time.Sleep(time.Duration(tryCount) * time.Second)
					continue
				}
				height++
				c.processTxs(listener, block.GetTransactions())
			}
		}
	}
	return nil
}

func (c *Controller) processBatchLogs(listener Listener, fromHeight, toHeight uint64) uint64 {
	var (
		contractAddresses []common.Address
	)
	chainId, _ := listener.GetChainID()
	addedContract := make(map[common.Address]struct{})
	filteredMethods := make(map[*abi.ABI]map[string]struct{})
	eventIds := make(map[common.Hash]string)
	for subscriptionName, subscription := range listener.GetSubscriptions() {
		name := subscription.Handler.Name
		if filteredMethods[subscription.Handler.ABI] == nil {
			filteredMethods[subscription.Handler.ABI] = make(map[string]struct{})
		}
		filteredMethods[subscription.Handler.ABI][name] = struct{}{}
		eventIds[subscription.Handler.ABI.Events[name].ID] = subscriptionName
		contractAddress := common.HexToAddress(subscription.To)

		if _, ok := addedContract[contractAddress]; !ok {
			contractAddresses = append(contractAddresses, contractAddress)
			addedContract[contractAddress] = struct{}{}
		}
	}
	retry := 0
	batchSize := uint64(listener.Config().GetLogsBatchSize)
	for fromHeight <= toHeight {
		if retry == 10 {
			break
		}
		opts := &bind.FilterOpts{
			Start:   fromHeight,
			Context: c.ctx,
		}
		if fromHeight+batchSize < toHeight {
			to := fromHeight + batchSize
			opts.End = &to
		} else {
			opts.End = &toHeight
		}
		logs, err := c.utilWrapper.FilterLogs(listener.GetEthClient(), opts, contractAddresses, filteredMethods)
		if err != nil {
			log.Error("[Controller][processBatchLogs] error while process batch logs", "err", err, "from", fromHeight, "to", opts.End)
			retry++
			continue
		}
		log.Info("[Controller][processBatchLogs] finish getting logs", "from", opts.Start, "to", *opts.End, "logs", len(logs), "listener", listener.GetName())
		fromHeight = *opts.End + 1
		for _, eventLog := range logs {
			eventId := eventLog.Topics[0]
			log.Info("[Controller][processBatchLogs] processing log", "topic", eventLog.Topics[0].Hex(), "address", eventLog.Address.Hex(), "transaction", eventLog.TxHash.Hex(), "listener", listener.GetName())
			if _, ok := eventIds[eventId]; !ok {
				continue
			}
			data := eventLog.Data
			name := eventIds[eventId]
			tx := NewEmptyTransaction(chainId, eventLog.TxHash, eventLog.Data, nil, &eventLog.Address)
			if job := listener.GetListenHandleJob(name, tx, eventId.Hex(), data); job != nil {
				if err := c.prepareJob(job); err != nil {
					log.Error("[Controller] failed on preparing job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
					metrics.Pusher.IncrCounter(metrics.PreparingFailedJobMetric, 1)
					continue
				}
				metrics.Pusher.IncrCounter(metrics.PreparingSuccessJobMetric, 1)
				metrics.Pusher.IncrGauge(metrics.ProcessingJobMetric, 1)
				c.JobChan <- job
			}
		}
		block, _ := listener.GetBlock(*opts.End)
		listener.UpdateCurrentBlock(block)
	}
	return fromHeight
}

func (c *Controller) processTxs(listener Listener, txs []Transaction) {
	for _, tx := range txs {
		if len(tx.GetData()) < 4 {
			continue
		}
		// get receipt and check tx status
		receipt, err := listener.GetReceipt(tx.GetHash())
		if err != nil || receipt.Status != 1 {
			continue
		}
		for name, subscription := range listener.GetSubscriptions() {
			if subscription.Handler == nil || subscription.Type != TxEvent {
				continue
			}
			eventId := tx.GetData()[0:4]
			data := tx.GetData()[4:]
			if job := listener.GetListenHandleJob(name, tx, common.Bytes2Hex(eventId), data); job != nil {
				c.PrepareJobChan <- job
			}
		}
	}
}

func (c *Controller) compareAddress(src, dst string) bool {
	// remove prefix (0x, ronin) and lower text
	src = strings.ToLower(strings.Replace(strings.Replace(src, "0x", "", 1), "ronin:", "", 1))
	dst = strings.ToLower(strings.Replace(strings.Replace(dst, "0x", "", 1), "ronin:", "", 1))
	return src == dst
}

func (c *Controller) LoadAbi(path string) (*abi.ABI, error) {
	if _, ok := c.HandlerABIs[path]; ok {
		return c.HandlerABIs[path], nil
	}
	a, err := c.utilWrapper.LoadAbi(path)
	if err != nil {
		return nil, err
	}
	c.HandlerABIs[path] = a
	return a, nil
}

func (c *Controller) Close() {
	// load isClosed
	val := c.isClosed.Load().(bool)
	if !val {
		log.Info("closing")
		c.cancelFunc()
	}
	c.Wait()
}
