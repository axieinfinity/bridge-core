package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	internal "github.com/axieinfinity/bridge-core"
	goeth "github.com/ethereum/go-ethereum"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
)

const (
	ACK_WITHDREW_TASK = "acknowledgeWithdrew"
	DEPOSIT_TASK      = "deposit"
	WITHDRAWAL_TASK   = "withdrawal"

	STATUS_PENDING    = "pending"
	STATUS_FAILED     = "failed"
	STATUS_PROCESSING = "processing"
	STATUS_DONE       = "done"

	GATEWAY_CONTRACT     = "Gateway"
	ETH_GATEWAY_CONTRACT = "EthGateway"
	BRIDGEADMIN_CONTRACT = "BridgeAdmin"
)

type EthereumListener struct {
	ctx       context.Context
	cancelCtx context.CancelFunc

	config *internal.LsConfig

	chainId *big.Int

	name           string
	period         time.Duration
	currentBlock   atomic.Value
	safeBlockRange uint64
	fromHeight     uint64
	utilsWrapper   utils.Utils
	client         utils.EthClient
	validatorSign  utils.ISign
	relayerSign    utils.ISign
	store          stores.MainStore

	prepareJobChan chan internal.JobHandler
	tasks          []internal.TaskHandler
}

func NewEthereumListener(ctx context.Context, cfg *internal.LsConfig, helpers utils.Utils, store stores.MainStore) (*EthereumListener, error) {
	newCtx, cancelFunc := context.WithCancel(ctx)
	ethListener := &EthereumListener{
		name:           cfg.Name,
		period:         cfg.LoadInterval,
		currentBlock:   atomic.Value{},
		ctx:            newCtx,
		cancelCtx:      cancelFunc,
		fromHeight:     cfg.FromHeight,
		utilsWrapper:   utils.NewUtils(),
		store:          store,
		config:         cfg,
		chainId:        hexutil.MustDecodeBig(cfg.ChainId),
		safeBlockRange: cfg.SafeBlockRange,
		tasks:          make([]internal.TaskHandler, 0),
	}
	if helpers != nil {
		ethListener.utilsWrapper = helpers
	}
	client, err := ethListener.utilsWrapper.NewEthClient(cfg.RpcUrl)
	if err != nil {
		log.Error(fmt.Sprintf("[New%sListener] error while dialing rpc client", cfg.Name), "err", err, "url", cfg.RpcUrl)
		return nil, err
	}
	ethListener.client = client

	if cfg.Secret.Validator != nil {
		ethListener.validatorSign, err = utils.NewSignMethod(cfg.Secret.Validator)
		if err != nil {
			log.Error(fmt.Sprintf("[New%sListener] error while getting validator key", cfg.Name), "err", err.Error())
			return nil, err
		}
	}
	if cfg.Secret.Relayer != nil {
		ethListener.relayerSign, err = utils.NewSignMethod(cfg.Secret.Relayer)
		if err != nil {
			log.Error(fmt.Sprintf("[New%sListener] error while getting relayer key", cfg.Name), "err", err.Error())
			return nil, err
		}
	}
	return ethListener, nil
}

func (e *EthereumListener) GetStore() stores.MainStore {
	return e.store
}

func (e *EthereumListener) Config() *internal.LsConfig {
	return e.config
}

func (e *EthereumListener) Start() {
	for _, task := range e.tasks {
		go task.Start()
	}
}

func (e *EthereumListener) GetName() string {
	return e.name
}

func (e *EthereumListener) Period() time.Duration {
	return e.period
}

func (e *EthereumListener) GetSafeBlockRange() uint64 {
	return e.safeBlockRange
}

func (e *EthereumListener) IsDisabled() bool {
	return e.config.Disabled
}

func (e *EthereumListener) SetInitHeight(height uint64) {
	e.fromHeight = height
}

func (e *EthereumListener) GetInitHeight() uint64 {
	return e.fromHeight
}

func (e *EthereumListener) GetTask(index int) internal.TaskHandler {
	return e.tasks[index]
}

func (e *EthereumListener) GetTasks() []internal.TaskHandler {
	return e.tasks
}

func (e *EthereumListener) AddTask(task internal.TaskHandler) {
	e.tasks = append(e.tasks, task)
}

func (e *EthereumListener) GetCurrentBlock() internal.Block {
	if _, ok := e.currentBlock.Load().(internal.Block); !ok {
		var (
			block internal.Block
			err   error
		)
		block, err = e.GetProcessedBlock()
		if err != nil {
			log.Error(fmt.Sprintf("[%sListener] error on getting processed block from database", e.GetName()), "err", err.Error())
			if e.fromHeight > 0 {
				block, err = e.GetBlock(e.fromHeight)
				if err != nil {
					log.Error(fmt.Sprintf("[%sListener] error on getting block from rpc", e.GetName()), "err", err, "fromHeight", e.fromHeight)
				}
			}
		}
		// if block is still nil, get latest block from rpc
		if block == nil {
			block, err = e.GetLatestBlock()
			if err != nil {
				log.Error(fmt.Sprintf("[%sListener] error on getting latest block from rpc", e.GetName()), "err", err.Error())
				return nil
			}
		}
		e.currentBlock.Store(block)
		return block
	}
	return e.currentBlock.Load().(internal.Block)
}

func (e *EthereumListener) IsUpTodate() bool {
	return true
}

func (e *EthereumListener) GetProcessedBlock() (internal.Block, error) {
	chainId, err := e.GetChainID()
	if err != nil {
		log.Error(fmt.Sprintf("[%sListener][GetLatestBlock] error while getting chainId", e.GetName()), "err", err.Error())
		return nil, err
	}
	encodedChainId := hexutil.EncodeBig(chainId)
	height, err := e.store.GetProcessedBlockStore().GetLatestBlock(encodedChainId)
	if err != nil {
		log.Error(fmt.Sprintf("[%sListener][GetLatestBlock] error while getting latest height from database", e.GetName()), "err", err, "chainId", encodedChainId)
		return nil, err
	}
	block, err := e.client.BlockByNumber(e.ctx, big.NewInt(height))
	if err != nil {
		return nil, err
	}
	return NewEthBlock(e.client, chainId, block, false)
}

func (e *EthereumListener) GetLatestBlock() (internal.Block, error) {
	block, err := e.client.BlockByNumber(e.ctx, nil)
	if err != nil {
		return nil, err
	}
	return NewEthBlock(e.client, e.chainId, block, false)
}

func (e *EthereumListener) GetLatestBlockHeight() (uint64, error) {
	return e.client.BlockNumber(e.ctx)
}

func (e *EthereumListener) GetChainID() (*big.Int, error) {
	if e.chainId != nil {
		return e.chainId, nil
	}
	return e.client.ChainID(e.ctx)
}

func (e *EthereumListener) Context() context.Context {
	return e.ctx
}

func (e *EthereumListener) GetSubscriptions() map[string]*internal.Subscribe {
	return e.config.Subscriptions
}

func (e *EthereumListener) UpdateCurrentBlock(block internal.Block) error {
	if block != nil && e.GetCurrentBlock().GetHeight() < block.GetHeight() {
		log.Info(fmt.Sprintf("[%sListener] UpdateCurrentBlock", e.name), "block", block.GetHeight())
		e.currentBlock.Store(block)
		return e.SaveCurrentBlockToDB()
	}
	return nil
}

func (e *EthereumListener) SaveCurrentBlockToDB() error {
	chainId, err := e.GetChainID()
	if err != nil {
		return err
	}

	if err := e.store.GetProcessedBlockStore().Save(hexutil.EncodeBig(chainId), int64(e.GetCurrentBlock().GetHeight())); err != nil {
		return err
	}

	return nil
}

func (e *EthereumListener) SaveTransactionsToDB(txs []internal.Transaction) error {
	return nil
}

func (e *EthereumListener) SetPrepareJobChan(jobChan chan internal.JobHandler) {
	e.prepareJobChan = jobChan
}

func (e *EthereumListener) GetEthClient() utils.EthClient {
	return e.client
}

func (e *EthereumListener) GetListenHandleJob(subscriptionName string, tx internal.Transaction, eventId string, data []byte) internal.JobHandler {
	// validate if data contains subscribed name
	subscription, ok := e.GetSubscriptions()[subscriptionName]
	if !ok {
		return nil
	}
	handlerName := subscription.Handler.Name
	if subscription.Type == internal.TxEvent {
		method, ok := subscription.Handler.ABI.Methods[handlerName]
		if !ok {
			return nil
		}
		if method.RawName != common.Bytes2Hex(data[0:4]) {
			return nil
		}
	} else if subscription.Type == internal.LogEvent {
		event, ok := subscription.Handler.ABI.Events[handlerName]
		if !ok {
			return nil
		}
		if hexutil.Encode(event.ID.Bytes()) != eventId {
			return nil
		}
	} else {
		return nil
	}
	return NewEthListenJob(internal.ListenHandler, e, subscriptionName, tx, data)
}

func (e *EthereumListener) SendCallbackJobs(listeners map[string]internal.Listener, subscriptionName string, tx internal.Transaction, inputData []byte) {
	log.Info(fmt.Sprintf("[%sListener][SendCallbackJobs] Start", e.GetName()), "subscriptionName", subscriptionName, "listeners", len(listeners), "fromTx", tx.GetHash().Hex())
	chainId, err := e.GetChainID()
	if err != nil {
		return
	}
	subscription, ok := e.GetSubscriptions()[subscriptionName]
	if !ok {
		log.Warn(fmt.Sprintf("[%sListener][SendCallbackJobs] cannot find subscription", e.GetName()), "subscriptionName", subscriptionName)
		return
	}
	log.Info(fmt.Sprintf("[%sListener][SendCallbackJobs] subscription found", e.GetName()), "subscriptionName", subscriptionName, "numberOfCallbacks", len(subscription.CallBacks))
	for listenerName, methodName := range subscription.CallBacks {
		log.Info(fmt.Sprintf("[%sListener][SendCallbackJobs] Loop through callbacks", e.GetName()), "subscriptionName", subscriptionName, "listenerName", listenerName, "methodName", methodName)
		l := listeners[listenerName]
		job := NewEthCallbackJob(l, methodName, tx, inputData, chainId, e.utilsWrapper)
		if job != nil {
			e.prepareJobChan <- job
		}
	}
}

func (e *EthereumListener) GetBlock(height uint64) (internal.Block, error) {
	block, err := e.client.BlockByNumber(e.ctx, big.NewInt(int64(height)))
	if err != nil {
		return nil, err
	}
	return NewEthBlock(e.client, e.chainId, block, false)
}

func (e *EthereumListener) GetBlockWithLogs(height uint64) (internal.Block, error) {
	block, err := e.client.BlockByNumber(e.ctx, big.NewInt(int64(height)))
	if err != nil {
		return nil, err
	}
	return NewEthBlock(e.client, e.chainId, block, true)
}

func (e *EthereumListener) GetReceipt(txHash common.Hash) (*ethtypes.Receipt, error) {
	return e.client.TransactionReceipt(e.ctx, txHash)
}

func (e *EthereumListener) NewJobFromDB(job *models.Job) (internal.JobHandler, error) {
	return newJobFromDB(e, job)
}

func (e *EthereumListener) Close() {
	e.client.Close()
	e.cancelCtx()
}

func (e *EthereumListener) GetValidatorSign() utils.ISign {
	return e.validatorSign
}

func (e *EthereumListener) GetRelayerSign() utils.ISign {
	return e.relayerSign
}

type EthBlock struct {
	block *ethtypes.Block
	txs   []internal.Transaction
	logs  []internal.Log
}

func NewEthBlock(client utils.EthClient, chainId *big.Int, block *ethtypes.Block, getLogs bool) (*EthBlock, error) {
	ethBlock := &EthBlock{
		block: block,
	}
	// convert txs into ILog
	for _, tx := range block.Transactions() {
		transaction, err := NewEthTransaction(chainId, tx)
		if err != nil {
			log.Error("[NewEthBlock] error while init new Eth Transaction", "err", err, "tx", tx.Hash().Hex())
			return nil, err
		}
		ethBlock.txs = append(ethBlock.txs, transaction)
	}
	if getLogs {
		log.Info("Getting logs from block hash", "block", block.NumberU64(), "hash", block.Hash().Hex())
		blockHash := block.Hash()
		logs, err := client.FilterLogs(context.Background(), goeth.FilterQuery{BlockHash: &blockHash})
		if err != nil {
			log.Error("[NewEthBlock] error while getting logs", "err", err, "block", block.NumberU64(), "hash", block.Hash().Hex())
			return nil, err
		}
		// convert logs to ILog
		for _, l := range logs {
			ethLog := EthLog(l)
			ethBlock.logs = append(ethBlock.logs, &ethLog)
		}
	}
	log.Info("[NewEthBlock] Finish getting eth block", "block", ethBlock.block.NumberU64(), "txs", len(ethBlock.txs), "logs", len(ethBlock.logs))
	return ethBlock, nil
}

func (b *EthBlock) GetHash() common.Hash { return b.block.Hash() }
func (b *EthBlock) GetHeight() uint64    { return b.block.NumberU64() }

func (b *EthBlock) GetTransactions() []internal.Transaction {
	return b.txs
}

func (b *EthBlock) GetLogs() []internal.Log {
	return b.logs
}

func (b *EthBlock) GetTimestamp() uint64 {
	return b.block.Time()
}

type EthTransaction struct {
	chainId *big.Int
	sender  common.Address
	tx      *ethtypes.Transaction
}

func NewEthTransactionWithoutError(chainId *big.Int, tx *ethtypes.Transaction) *EthTransaction {
	ethTx := &EthTransaction{
		chainId: chainId,
		tx:      tx,
	}
	sender, err := ethtypes.LatestSignerForChainID(chainId).Sender(tx)
	if err == nil {
		ethTx.sender = sender
	}
	return ethTx
}

func NewEthTransaction(chainId *big.Int, tx *ethtypes.Transaction) (*EthTransaction, error) {
	sender, err := ethtypes.LatestSignerForChainID(chainId).Sender(tx)
	if err != nil {
		return nil, err
	}
	return &EthTransaction{
		chainId: chainId,
		sender:  sender,
		tx:      tx,
	}, nil
}

func (b *EthTransaction) GetHash() common.Hash {
	return b.tx.Hash()
}

func (b *EthTransaction) GetFromAddress() string {
	return b.sender.Hex()
}
func (b *EthTransaction) GetToAddress() string {
	if b.tx.To() != nil {
		return b.tx.To().Hex()
	}
	return ""
}

func (b *EthTransaction) GetData() []byte {
	return b.tx.Data()
}

func (b *EthTransaction) GetValue() *big.Int {
	return b.tx.Value()
}

type EmptyTransaction struct {
	chainId  *big.Int
	hash     common.Hash
	from, to *common.Address
	data     []byte
}

func NewEmptyTransaction(chainId *big.Int, tx common.Hash, data []byte, from, to *common.Address) *EmptyTransaction {
	return &EmptyTransaction{
		chainId: chainId,
		hash:    tx,
		from:    from,
		to:      to,
		data:    data,
	}
}

func (b *EmptyTransaction) GetHash() common.Hash {
	return b.hash
}

func (b *EmptyTransaction) GetFromAddress() string {
	if b.from != nil {
		return b.from.Hex()
	}
	return ""
}
func (b *EmptyTransaction) GetToAddress() string {
	if b.to != nil {
		return b.to.Hex()
	}
	return ""
}

func (b *EmptyTransaction) GetData() []byte {
	return b.data
}

func (b *EmptyTransaction) GetValue() *big.Int {
	return nil
}

type EthLog ethtypes.Log

func (e *EthLog) GetContractAddress() string {
	return e.Address.Hex()
}

func (e *EthLog) GetTopics() (topics []string) {
	for _, topic := range e.Topics {
		topics = append(topics, topic.Hex())
	}
	return
}

func (e *EthLog) GetData() []byte {
	return e.Data
}

func (e *EthLog) GetIndex() uint {
	return e.Index
}

func (e *EthLog) GetTxIndex() uint {
	return e.TxIndex
}

func (e *EthLog) GetTransactionHash() string {
	return e.TxHash.Hex()
}

type EthListenJob struct {
	*internal.BaseJob
}

func NewEthListenJob(jobType int, listener internal.Listener, subscriptionName string, tx internal.Transaction, data []byte) internal.JobHandler {
	chainId, err := listener.GetChainID()
	if err != nil {
		return nil
	}
	job := &models.Job{
		ID:               0,
		SubscriptionName: subscriptionName,
		Type:             jobType,
		RetryCount:       0,
		Data:             common.Bytes2Hex(data),
		FromChainId:      hexutil.EncodeBig(chainId),
	}
	baseJob, err := internal.NewBaseJob(listener, job, tx)
	if err != nil {
		return nil
	}
	return &EthListenJob{
		BaseJob: baseJob,
	}
}

type EthCallbackJob struct {
	*internal.BaseJob
	method    string
	createdAt time.Time
}

func NewEthCallbackJob(listener internal.Listener, method string, tx internal.Transaction, data []byte, fromChainID *big.Int, helpers utils.Utils) *EthCallbackJob {
	job := &models.Job{
		ID:          0,
		Type:        internal.CallbackHandler,
		RetryCount:  0,
		Data:        common.Bytes2Hex(data),
		FromChainId: hexutil.EncodeBig(fromChainID),
	}
	baseJob, err := internal.NewBaseJob(listener, job, tx)
	if err != nil {
		return nil
	}

	return &EthCallbackJob{
		BaseJob: baseJob,
		method:  method,
	}
}

func (e *EthCallbackJob) Process() ([]byte, error) {
	log.Info("[EthCallbackJob] Start Process", "method", e.method, "jobId", e.GetID())
	val, err := e.Utils().Invoke(e.GetListener(), e.method, e.FromChainID(), e.GetTransaction(), e.GetData())
	if err != nil {
		return nil, err
	}
	invokeErr, ok := val.Interface().(error)
	if ok {
		return nil, invokeErr
	}
	return nil, nil
}

func (e *EthCallbackJob) Update(status string) error {
	job := &models.Job{
		ID:               int(e.GetID()),
		Listener:         e.GetListener().GetName(),
		SubscriptionName: e.GetSubscriptionName(),
		Type:             e.GetType(),
		RetryCount:       e.GetRetryCount(),
		Status:           status,
		Data:             common.Bytes2Hex(e.GetData()),
		Transaction:      e.GetTransaction().GetHash().Hex(),
		CreatedAt:        time.Now().Unix(),
		FromChainId:      hexutil.EncodeBig(e.FromChainID()),
		Method:           e.method,
	}
	if err := e.GetListener().GetStore().GetJobStore().Update(job); err != nil {
		return err
	}
	return nil
}

func (e *EthCallbackJob) Save() error {
	job := &models.Job{
		Listener:         e.GetListener().GetName(),
		SubscriptionName: e.GetSubscriptionName(),
		Type:             e.GetType(),
		RetryCount:       e.GetRetryCount(),
		Status:           STATUS_PENDING,
		Data:             common.Bytes2Hex(e.GetData()),
		Transaction:      e.GetTransaction().GetHash().Hex(),
		CreatedAt:        time.Now().Unix(),
		FromChainId:      hexutil.EncodeBig(e.FromChainID()),
		Method:           e.method,
	}
	if err := e.GetListener().GetStore().GetJobStore().Save(job); err != nil {
		return err
	}
	e.SetID(int32(job.ID))
	e.createdAt = time.Unix(job.CreatedAt, 0)
	return nil
}

func (e *EthCallbackJob) CreatedAt() time.Time {
	return e.createdAt
}

func executeTemplate(ts string, data map[string]interface{}) (string, error) {
	t := template.New("template")
	sb := strings.Builder{}
	t.Parse(ts)

	if err := t.Execute(&sb, data); err != nil {
		return "", err
	}

	return sb.String(), nil
}

func newJobFromDB(listener internal.Listener, job *models.Job) (internal.JobHandler, error) {
	chainId, err := hexutil.DecodeBig(job.FromChainId)
	if err != nil {
		return nil, err
	}
	// get transaction from hash
	tx, _, err := listener.GetEthClient().TransactionByHash(context.Background(), common.HexToHash(job.Transaction))
	if err != nil {
		return nil, err
	}
	transaction, err := NewEthTransaction(chainId, tx)
	if err != nil {
		return nil, err
	}
	baseJob, err := internal.NewBaseJob(listener, job, transaction)
	if err != nil {
		return nil, err
	}
	switch job.Type {
	case internal.ListenHandler:
		return &EthListenJob{
			BaseJob: baseJob,
		}, nil
	case internal.CallbackHandler:
		if job.Method == "" {
			return nil, nil
		}
		return &EthCallbackJob{
			BaseJob: baseJob,
			method:  job.Method,
		}, nil
	}
	return nil, errors.New("jobType does not match")
}
