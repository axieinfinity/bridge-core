package bridge_core

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"strings"
	"time"

	bridge_contracts "github.com/axieinfinity/bridge-contracts"
	"github.com/axieinfinity/bridge-core/adapters"
	"github.com/axieinfinity/bridge-core/metrics"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/sony/gobreaker"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"gorm.io/gorm"
)

const (
	defaultBatchSize    = 100
	defaultMaxRetry     = 10
	defaultTaskInterval = 3
)

var listeners map[string]func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener

func init() {
	listeners = make(map[string]func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener)
}

func AddListener(name string, initFunc func(ctx context.Context, lsConfig *LsConfig, store stores.MainStore, helpers utils.Utils) Listener) {
	listeners[name] = initFunc
}

type Controller struct {
	ctx        context.Context
	cancelFunc context.CancelFunc

	listeners   map[string]Listener
	HandlerABIs map[string]*abi.ABI
	utilWrapper utils.Utils

	Pool *Pool
	cfg  *Config

	store               stores.MainStore
	hasSubscriptionType map[string]map[int]bool

	processingFrame int64
	cb              *gobreaker.CircuitBreaker
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
		processingFrame:     time.Now().Unix(),
		store:               stores.NewMainStore(db),
		hasSubscriptionType: make(map[string]map[int]bool),
		Pool:                NewPool(ctx, cfg, db, nil),
		cb: gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:     "process pending jobs",
			Interval: 60 * time.Second,
			Timeout:  60 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return (counts.Requests > 10 && failureRatio >= 0.8) || counts.ConsecutiveFailures > 5
			},
		}),
	}

	if adapters.AppConfig.Prometheus.TurnOn {
		metrics.RunPusher(ctx)
	}

	if helpers != nil {
		c.utilWrapper = helpers
	}

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
		l.SetPrepareJobChan(c.Pool.PrepareJobChan)

		// set listeners to listeners
		l.AddListeners(c.listeners)

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
	var workers []Worker
	// init workers
	for i := 0; i < cfg.NumberOfWorkers; i++ {
		w := NewWorker(ctx, i, c.Pool.PrepareJobChan, c.Pool.FailedJobChan, c.Pool.Queue, c.Pool.MaxQueueSize, c.listeners)
		workers = append(workers, w)
	}
	c.Pool.AddWorkers(workers)
	return c, nil
}

// LoadABIsFromConfig loads all ABIPath and add results to Handler.ABI
func (c *Controller) LoadABIsFromConfig(lsConfig *LsConfig) (err error) {
	for _, subscription := range lsConfig.Subscriptions {
		// if contract is not defined or abi is not nil then do nothing
		if subscription.Handler.Contract == "" || subscription.Handler.ABI != nil {
			continue
		}
		// load abi for handler
		if subscription.Handler.ABI, err = bridge_contracts.ABIMaps[subscription.Handler.Contract].GetAbi(); err != nil {
			return err
		}
	}
	return
}

func (c *Controller) Start() error {
	go c.Pool.Start(c.closeListeners)
	go c.processPendingJobs()
	c.startListeners()
	return nil
}

func (c *Controller) processPendingJobs() {
	ticker := time.NewTicker(time.Minute)
	var listeners []string
	for _, v := range c.listeners {
		listeners = append(listeners, v.GetName())
	}
	if len(listeners) == 0 {
		return
	}
	for {
		select {
		case <-ticker.C:
			jobs, err := c.store.GetJobStore().SearchJobs(&stores.SearchJobs{
				Status:       stores.STATUS_PENDING,
				MaxCreatedAt: c.processingFrame,
				Listeners:    listeners,
				Limit:        200,
			})
			if err != nil {
				// just log and do nothing.
				log.Error("[Controller] error while getting pending jobs from database", "err", err)
			}
			if len(jobs) == 0 {
				return
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
				ji, err := c.cb.Execute(func() (interface{}, error) {
					j, err := listener.NewJobFromDB(job)
					if err != nil {
						log.Error("[Controller] error while init job from db", "err", err, "jobId", job.ID, "type", job.Type)
						return nil, err
					}
					return j, nil
				})
				if err == gobreaker.ErrOpenState {
					log.Info("Processing pending jobs failed too many times, break")
					break
				}

				j, _ := ji.(JobHandler)
				// add job to jobChan
				if j != nil {
					c.Pool.PrepareJobChan <- j
					job.Status = stores.STATUS_PROCESSED
					c.store.GetJobStore().Update(job)
				}
			}
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

// startListener starts listening events for a listener, it comes with a tryCount which close this listener if tryCount reaches 10 times
func (c *Controller) startListening(listener Listener, tryCount int) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("[Controller][startListener] recover from panic", "message", r, "trace", string(debug.Stack()))
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
		c.startListening(listener, tryCount+1)
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
			c.startListening(listener, tryCount+1)
			return
		}
	}

	// start stats reporter
	statsTick := time.NewTicker(2 * time.Second)
	go func() {
		select {
		case <-statsTick.C:
			stats := c.Pool.Stats()
			log.Info("[Controller] pool stats", "pending", stats.PendingQueue, "queue", stats.Queue)
		}
	}()

	// start listening to block's events
	tick := time.NewTicker(listener.Period())
	for {
		select {
		case <-listener.Context().Done():
			return
		case <-tick.C:
			// stop if the pool is closed
			if c.Pool.IsClosed() {
				// wait for pool is totally shutdown
				c.Pool.Wait()
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
	chainId, err := listener.GetChainID()
	if err != nil {
		log.Error("[Controller][processBatchLogs] error while getting chainID", "err", err, "listener", listener.GetName())
		return fromHeight
	}
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
	for fromHeight < toHeight {
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
		} else if fromHeight == toHeight-1 {
			opts.End = &fromHeight
		} else {
			to := toHeight - 1
			opts.End = &to
		}
		logs, err := c.utilWrapper.FilterLogs(listener.GetEthClient(), opts, contractAddresses, filteredMethods)
		if err != nil {
			log.Error("[Controller][processBatchLogs] error while process batch logs", "err", err, "from", fromHeight, "to", opts.End)
			retry++
			continue
		}
		log.Info("[Controller][processBatchLogs] finish getting logs", "from", opts.Start, "to", *opts.End, "logs", len(logs), "listener", listener.GetName())
		fromHeight = *opts.End + 1
		for i, eventLog := range logs {
			eventId := eventLog.Topics[0]
			log.Info("[Controller][processBatchLogs] processing log", "topic", eventLog.Topics[0].Hex(), "address", eventLog.Address.Hex(), "transaction", eventLog.TxHash.Hex(), "listener", listener.GetName())
			if _, ok := eventIds[eventId]; !ok {
				continue
			}
			data, err := json.Marshal(eventLog)
			if err != nil {
				log.Error("[Controller] error while marshalling log", "err", err, "transaction", eventLog.TxHash.Hex(), "index", i)
				continue
			}
			name := eventIds[eventId]
			tx := NewEmptyTransaction(chainId, eventLog.TxHash, eventLog.Data, nil, &eventLog.Address)
			if job := listener.GetListenHandleJob(name, tx, eventId.Hex(), data); job != nil {
				if err := c.Pool.PrepareJob(job); err != nil {
					log.Error("[Controller] failed on preparing job", "err", err, "jobType", job.GetType(), "tx", job.GetTransaction().GetHash().Hex())
					metrics.Pusher.IncrCounter(metrics.PreparingFailedJobMetric, 1)
					continue
				}
				metrics.Pusher.IncrCounter(metrics.PreparingSuccessJobMetric, 1)
				c.Pool.JobChan <- job
			}
		}
		block, _ := listener.GetBlock(fromHeight)
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
				c.Pool.PrepareJobChan <- job
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
	val := c.Pool.IsClosed()
	if !val {
		log.Info("closing")
		c.cancelFunc()
	}
	c.Pool.Wait()
}
