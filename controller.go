package bridge_core

import (
	"context"
	"fmt"
	"math/big"
	"time"

	bridge_contracts "github.com/axieinfinity/bridge-contracts"
	"github.com/axieinfinity/bridge-core/orchestrators"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/types"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/sony/gobreaker"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	defaultBatchSize = 100
	defaultMaxRetry  = 10
)

// var listeners map[string]func(ctx context.Context, lsConfig *types.LsConfig, store stores.MainStore, helpers utils.Utils) types.Listener

// func init() {
// 	listeners = make(map[string]func(ctx context.Context, lsConfig *types.LsConfig, store stores.MainStore, helpers utils.Utils) types.Listener)
// }

// func AddListener(name string, initFunc func(ctx context.Context, lsConfig *types.LsConfig, store stores.MainStore, helpers utils.Utils) types.Listener) {
// 	listeners[name] = initFunc
// }

type Controller struct {
	clients     map[string]types.ChainClient
	HandlerABIs map[string]*abi.ABI
	utilWrapper utils.Utils

	cfg             *types.Config
	getLogBatchSize int

	store               stores.MainStore
	hasSubscriptionType map[string]map[int]bool
	subscriptions       map[string]*types.Subscribe

	processingFrame int64
	cb              *gobreaker.CircuitBreaker

	logOrchestrator         orchestrators.LogOrchestrator
	transactionOrchestrator orchestrators.TransactionOrchestrator

	currentHeights map[*big.Int]uint64 // map chain id -> processed block height
}

func New(
	store stores.MainStore,
	clients map[string]types.ChainClient,
	subscriptions map[string]*types.Subscribe,
	logOrchestrator orchestrators.LogOrchestrator,
	transactionOrchestrator orchestrators.TransactionOrchestrator,
) *Controller {
	// if cfg.NumberOfWorkers <= 0 {
	// 	cfg.NumberOfWorkers = types.DefaultWorkers
	// }

	c := &Controller{
		clients:             clients,
		HandlerABIs:         make(map[string]*abi.ABI),
		utilWrapper:         utils.NewUtils(),
		processingFrame:     time.Now().Unix(),
		store:               store,
		hasSubscriptionType: make(map[string]map[int]bool),
		subscriptions:       subscriptions,
		cb: gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:     "process pending jobs",
			Interval: 60 * time.Second,
			Timeout:  60 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return (counts.Requests > 10 && failureRatio >= 0.8) || counts.ConsecutiveFailures > 5
			},
		}),
		logOrchestrator:         logOrchestrator,
		transactionOrchestrator: transactionOrchestrator,
		getLogBatchSize:         defaultBatchSize,
		currentHeights:          make(map[*big.Int]uint64),
	}

	for k, v := range subscriptions {
		if _, ok := c.hasSubscriptionType[k][v.Type]; !ok {
			c.hasSubscriptionType[k] = map[int]bool{}
		}
		c.hasSubscriptionType[k][v.Type] = true
	}

	// if adapters.AppConfig.Prometheus.TurnOn {
	// 	metrics.RunPusher(ctx)
	// }

	// if helpers != nil {
	// 	c.utilWrapper = helpers
	// }

	// add listeners from config
	// for name, lsConfig := range c.cfg.Listeners {
	// 	if lsConfig.LoadInterval <= 0 {
	// 		lsConfig.LoadInterval = defaultTaskInterval
	// 	}
	// 	lsConfig.LoadInterval *= time.Second
	// 	lsConfig.Name = name

	// 	// load abi from lsConfig
	// 	if err := c.LoadABIsFromConfig(lsConfig); err != nil {
	// 		return nil, err
	// 	}

	// 	// Invoke init function which is based on listener's name
	// 	initFunc, ok := listeners[name]
	// 	if !ok {
	// 		continue
	// 	}
	// 	l := initFunc(c.ctx, lsConfig, c.store, c.utilWrapper, c.Pool)
	// 	if l == nil {
	// 		return nil, errors.New("listener is nil")
	// 	}

	// 	// set listeners to listeners
	// 	l.AddListeners(c.listeners)

	// 	// add listener to controller
	// 	c.listeners[name] = l
	// 	c.hasSubscriptionType[name] = make(map[int]bool)

	// 	if lsConfig.GetLogsBatchSize == 0 {
	// 		lsConfig.GetLogsBatchSize = defaultBatchSize
	// 	}

	// 	// filtering subscription, get all subscriptionType available for each listener
	// 	for _, subscription := range l.GetSubscriptions() {
	// 		if c.hasSubscriptionType[name][subscription.Type] {
	// 			continue
	// 		}
	// 		c.hasSubscriptionType[name][subscription.Type] = true
	// 	}
	// }

	// var workers []types.Worker
	// // init workers
	// for i := 0; i < cfg.NumberOfWorkers; i++ {
	// 	w := types.NewWorker(ctx, i, c.Pool.MaxQueueSize, c.listeners)
	// 	workers = append(workers, w)
	// }
	// c.Pool.AddWorkers(workers)
	return c
}

// LoadABIsFromConfig loads all ABIPath and add results to Handler.ABI
func (c *Controller) LoadABIs() (err error) {
	for _, subscription := range c.subscriptions {
		if subscription.Handler.Contract == "" || subscription.Handler.ABI != nil {
			continue
		}
		if subscription.Handler.ABI, err = bridge_contracts.ABIMaps[subscription.Handler.Contract].GetAbi(); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) Start(ctx context.Context) error {
	go c.logOrchestrator.Start(ctx)
	go c.transactionOrchestrator.Start(ctx)

	c.processPendingJobs(ctx)
	c.startListeners(ctx)
	return nil
}

func (c *Controller) processPendingJobs(ctx context.Context) {
	var listeners []string
	for _, v := range c.clients {
		listeners = append(listeners, v.GetName())
	}
	if len(listeners) == 0 {
		return
	}
	for {
		// stop processing if controller is closed
		select {
		case <-ctx.Done():
			return
		default:
			// do nothing
		}
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

		var (
			logJobs         = make([]types.Job[types.Log], 0)
			transactionJobs = make([]types.Job[types.Transaction], 0)
		)
		for _, job := range jobs {

			switch job.Type {
			case types.JobTypeLog:

				logJobs = append(logJobs, types.Job[types.Log]{})
			case types.JobTypeTransaction:

			}
			// create listener
			_ = job
			// listener, ok := c.listeners[job.Listener]
			// if !ok || listener.IsDisabled() {
			// 	continue
			// }
			// if job.Type == types.CallbackHandler && job.Method == "" {
			// 	// invalid job, update it to failed
			// 	job.Status = stores.STATUS_FAILED
			// 	if err = listener.GetStore().GetJobStore().Update(job); err != nil {
			// 		log.Error("[Controller] error while updating invalid job", "err", err, "id", job.ID)
			// 	}
			// 	continue
			// }
			// ji, err := c.cb.Execute(func() (interface{}, error) {
			// 	j, err := listener.NewJobFromDB(job)
			// 	if err != nil {
			// 		log.Error("[Controller] error while init job from db", "err", err, "jobId", job.ID, "type", job.Type)
			// 		return nil, err
			// 	}
			// 	return j, nil
			// })
			// if err == gobreaker.ErrOpenState {
			// 	log.Info("Processing pending jobs failed too many times, break")
			// 	break
			// }

			// j, _ := ji.(types.Job)
			// // add job to jobChan
			// if j != nil {
			// 	c.logOrchestrator.Process(context.Background(), j)
			// 	c.Pool.Enqueue(j)
			// 	job.Status = stores.STATUS_PROCESSED
			// 	c.store.GetJobStore().Update(job)
			// }
		}

		if len(logJobs) > 0 {
			c.logOrchestrator.Process(ctx, logJobs...)
		}
		if len(transactionJobs) > 0 {
			c.transactionOrchestrator.Process(ctx, transactionJobs...)
		}
	}

}

func (c *Controller) startListeners(ctx context.Context) {
	// make sure all listeners are up-to-date
	for _, client := range c.clients {
		go c.startListening(ctx, client, 0)
	}
}

func (c *Controller) gatherStats(ctx context.Context) {
	tick := time.NewTicker(time.Duration(2) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			stats := c.logOrchestrator.Stats().Merge(c.transactionOrchestrator.Stats())
			log.Info("[Controller] orchestrator stats", "retry", stats.RetryableQueue, "queue", stats.Queue, "retryingJob", stats.RetryingJob)
		}
	}
}

func (c *Controller) getCurrentHeight(ctx context.Context, client types.ChainClient) (uint64, error) {
	var (
		height int64
		err    error
	)

	chainID, err := client.GetChainID()
	if err != nil {
		return 0, err
	}

	if v, ok := c.currentHeights[chainID]; ok {
		return v, nil
	}

	id := fmt.Sprintf("0x%x", chainID)
	height, err = c.store.GetProcessedBlockStore().GetLatestBlock(id)
	if height != -1 && err != nil {
		return 0, err
	}

	if height == -1 {
		if chainHeight, err := client.GetLatestBlockHeight(ctx); err == nil {
			c.currentHeights[chainID] = chainHeight
			return chainHeight, nil
		}
	}

	return 0, nil
}

// startListener starts listening events for a listener, it comes with a tryCount which close this listener if tryCount reaches 10 times
func (c *Controller) startListening(ctx context.Context, client types.ChainClient, tryCount int) {
	// start listening to block's events
	tick := time.NewTicker(client.Period())
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			log.Info("ehhh")
			latestHeight, err := client.GetLatestBlockHeight(ctx)
			if err != nil {
				log.Error("[Controller][Watcher] error while get latest block height", "err", err)
				continue
			}
			currentHeight, err := c.getCurrentHeight(ctx, client)

			// do nothing if currentBlock is within safe block range
			if err != nil || currentHeight > latestHeight-client.GetSafeBlockRange() {
				continue
			}
			// if current block is behind safeBlockRange then process without waiting
			if err := c.processBehindBlock(ctx, client, currentHeight, latestHeight); err != nil {
				log.Error("[Controller][Watcher] error while processing behind block", "err", err, "height", currentHeight, "latestHeight", latestHeight)
				continue
			}
		}
	}
}

func (c *Controller) processBehindBlock(ctx context.Context, client types.ChainClient, height, latestBlockHeight uint64) error {
	if latestBlockHeight-client.GetSafeBlockRange() > height {
		var (
			safeBlock, block  types.Block
			tryCount          int
			err               error
			processedToHeight uint64
		)
		safeBlock, err = client.GetBlock(ctx, latestBlockHeight-client.GetSafeBlockRange())
		if err != nil {
			log.Error("[Controller][Process] error while getting safeBlock", "err", err, "latest", latestBlockHeight)
			return err
		}
		// process logs
		if c.hasSubscriptionType[client.GetName()][types.JobTypeLog] {
			processedToHeight = c.processBatchLogs(ctx, client, height, safeBlock.GetHeight())
		}
		// process transactions
		if c.hasSubscriptionType[client.GetName()][types.JobTypeTransaction] {
			for height <= processedToHeight {
				block, err = client.GetBlock(ctx, height)
				if err != nil {
					log.Error("[Controller][processBlock] error while get block", "err", err, "listener", client.GetName(), "height", height)
					tryCount++
					time.Sleep(time.Duration(tryCount) * time.Second)
					continue
				}
				height++
				c.processTxs(ctx, client, block.GetTransactions())
				// c.processTxs(listener, block.GetTrans actions())
			}
		}
	}
	return nil
}

func (c *Controller) processBatchLogs(ctx context.Context, client types.ChainClient, fromHeight, toHeight uint64) uint64 {
	var (
		contractAddresses []common.Address
	)
	chainId, err := client.GetChainID()
	if err != nil {
		log.Error("[Controller][processBatchLogs] error while getting chainID", "err", err, "listener", client.GetName())
		return fromHeight
	}
	chainIDHex := fmt.Sprintf("0x%x", chainId)

	addedContract := make(map[common.Address]struct{})
	filteredMethods := make(map[*abi.ABI]map[string]struct{})
	eventIds := make(map[common.Hash]string)
	for _, subscription := range c.subscriptions {
		name := subscription.Handler.Name
		if filteredMethods[subscription.Handler.ABI] == nil {
			filteredMethods[subscription.Handler.ABI] = make(map[string]struct{})
		}
		filteredMethods[subscription.Handler.ABI][name] = struct{}{}
		eventIds[subscription.Handler.ABI.Events[name].ID] = name
		contractAddress := common.HexToAddress(subscription.To)

		if _, ok := addedContract[contractAddress]; !ok {
			contractAddresses = append(contractAddresses, contractAddress)
			addedContract[contractAddress] = struct{}{}
		}
	}
	retry := 0

	for fromHeight < toHeight {
		if retry == 10 {
			break
		}

		var to uint64
		if fromHeight+uint64(c.getLogBatchSize) < toHeight {
			to = fromHeight + uint64(c.getLogBatchSize)
		} else if fromHeight == toHeight-1 {
			to = fromHeight
		} else {
			to = toHeight - 1
		}

		topics, err := utils.MakeTopics(filteredMethods)
		if err != nil {
			log.Error("[Controller][processBatchLogs] error while process batch logs", "err", err, "from", fromHeight, "to", to)
			retry++
			continue
		}
		logs, err := client.GetLogs(ctx, &types.GetLogsOption{
			From:      big.NewInt(0).SetUint64(fromHeight),
			To:        big.NewInt(0).SetUint64(to),
			Addresses: contractAddresses,
			Topics:    topics,
		})

		if err != nil {
			retry++
			log.Error("[Controller][processBatchLogs] error while process batch logs", "err", err, "from", fromHeight, "to", to)
			continue
		}

		log.Trace("[Controller][processBatchLogs] finish getting logs", "from", fromHeight, "to", toHeight, "logs", len(logs), "listener", client.GetName())
		jobs := make([]types.Job[types.Log], 0)
		jobs = append(jobs, types.Job[types.Log]{
			Type:  types.JobTypeLog,
			Name:  client.GetName(),
			Event: "Swap",
			Data:  &types.LogData{},
		})
		for _, eventLog := range logs {
			topic := eventLog.GetTopics()[0]

			job := types.Job[types.Log]{
				Type:  types.JobTypeLog,
				Name:  client.GetName(),
				Event: eventIds[common.HexToHash(topic)],
				Data:  eventLog,
			}

			jobs = append(jobs, job)

		}

		// enqueue jobs
		c.logOrchestrator.Process(ctx, jobs...)

		// update from height, and also update again from height's block
		fromHeight = to + 1
		if err := c.store.GetProcessedBlockStore().Save(chainIDHex, int64(fromHeight)); err != nil {
			log.Error("[Controller] error while saving processed block", "err", err, "chainID", chainId.String(), "from", fromHeight)
			continue
		}

		c.currentHeights[chainId] = fromHeight

	}
	return fromHeight
}

func (c *Controller) processTxs(ctx context.Context, client types.ChainClient, txs []types.Transaction) {
	for _, tx := range txs {
		if len(tx.GetData()) < 4 {
			continue
		}
		// get receipt and check tx status
		receipt, err := client.GetReceipt(ctx, tx.GetHash())
		if err != nil || !receipt.GetStatus() {
			continue
		}
		// filter events
		for name, subscription := range c.subscriptions {
			if subscription.Handler == nil || subscription.Type != types.JobTypeLog {
				continue
			}
			// eventId := tx.GetData()[0:4]
			// data := tx.GetData()[4:]
			job := types.Job[types.Transaction]{
				Type:  types.JobTypeTransaction,
				Name:  client.GetName(),
				Event: name,
				Data:  tx,
			}

			c.transactionOrchestrator.Process(ctx, job)
		}
	}
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

func (c *Controller) Close(ctx context.Context) {
	c.logOrchestrator.Stop(ctx)
	c.transactionOrchestrator.Stop(ctx)

	for _, v := range c.clients {
		v.Close(ctx)
	}
}
