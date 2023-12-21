package bridge_core

import (
	"context"
	"math/big"
	"time"

	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	ListenHandler = iota
	CallbackHandler
)

const (
	TxEvent = iota
	LogEvent
)

type Listener interface {
	GetName() string
	GetStore() stores.MainStore
	Config() *LsConfig

	Period() time.Duration
	GetSafeBlockRange() uint64
	GetCurrentBlock() Block
	GetLatestBlock() (Block, error)
	GetLatestBlockHeight() (uint64, error)
	GetBlock(height uint64) (Block, error)
	GetBlockWithLogs(height uint64) (Block, error)
	GetChainID() (*big.Int, error)
	GetReceipt(common.Hash) (*types.Receipt, error)
	Context() context.Context

	GetSubscriptions() map[string]*Subscribe

	UpdateCurrentBlock(block Block) error

	SaveCurrentBlockToDB() error
	SaveTransactionsToDB(txs []Transaction) error

	GetListenHandleJob(subscriptionName string, tx Transaction, eventId string, data []byte) JobHandler
	SendCallbackJobs(listeners map[string]Listener, subscriptionName string, tx Transaction, inputData []byte)

	NewJobFromDB(job *models.Job) (JobHandler, error)

	Start()
	Close()

	IsDisabled() bool
	SetInitHeight(uint64)
	GetInitHeight() uint64

	GetEthClient() utils.EthClient

	GetTasks() []TaskHandler
	GetTask(index int) TaskHandler
	AddTask(handler TaskHandler)

	IsUpTodate() bool

	GetBridgeOperatorSign() utils.ISign
	GetVoterSign() utils.ISign
	GetRelayerSign() utils.ISign
	GetLegacyBridgeOperatorSign() utils.ISign

	AddListeners(map[string]Listener)

	// GetListener returns listener by name
	GetListener(string) Listener
	CacheBlocks(blockNumbers map[uint64]struct{})
}

type Transaction interface {
	GetHash() common.Hash
	GetFromAddress() string
	GetToAddress() string
	GetData() []byte
	GetValue() *big.Int
	BlockNumber() uint64
}

type Log interface {
	GetContractAddress() string
	GetTopics() []string
	GetData() []byte
	GetIndex() uint
	GetTxIndex() uint
	GetTransactionHash() string
}

type Receipt interface {
	GetTransaction() Transaction
	GetStatus() bool
	GetLogs() []Log
}

type Block interface {
	GetHash() common.Hash
	GetHeight() uint64
	GetTransactions() []Transaction
	GetLogs() []Log
	GetTimestamp() uint64
}

type JobHandler interface {
	GetID() int32
	GetType() int
	GetRetryCount() int
	GetNextTry() int64
	GetMaxTry() int
	GetData() []byte
	GetValue() *big.Int
	GetBackOff() int

	Process() ([]byte, error)
	Hash() common.Hash

	IncreaseRetryCount()
	UpdateNextTry(int64)

	GetListener() Listener
	GetSubscriptionName() string
	GetTransaction() Transaction

	FromChainID() *big.Int

	Save(string) error

	CreatedAt() time.Time
	String() string
}

type TaskHandler interface {
	Start()
	Close()
	GetListener() Listener
	SetLimitQuery(limit int)
}

type Config struct {
	Listeners       map[string]*LsConfig `json:"listeners" mapstructure:"listeners"`
	NumberOfWorkers int                  `json:"numberOfWorkers" mapstructure:"numberOfWorkers"`
	MaxQueueSize    int                  `json:"maxQueueSize" mapstructure:"maxQueueSize"`
	MaxRetry        int32                `json:"maxRetry" mapstructure:"maxRetry"`
	BackOff         int32                `json:"backoff" mapstructure:"backoff"`
	DB              *stores.Database     `json:"database" mapstructure:"database"`

	// this field is used for testing purpose
	Testing bool
}

type LsConfig struct {
	ChainId        string        `json:"chainId" mapstructure:"chainId"`
	Name           string        `json:"-"`
	RpcUrl         string        `json:"rpcUrl" mapstructure:"rpcUrl"`
	LoadInterval   time.Duration `json:"blockTime" mapstructure:"blockTime"`
	SafeBlockRange uint64        `json:"safeBlockRange" mapstructure:"safeBlockRange"`
	FromHeight     uint64        `json:"fromHeight" mapstructure:"fromHeight"`
	TaskInterval   time.Duration `json:"taskInterval" mapstructure:"taskInterval"`
	Disabled       bool          `json:"disabled" mapstructure:"disabled"`

	// TODO: apply more ways to get privatekey. such as: PLAINTEXT, KMS, etc.
	Secret                 *Secret               `json:"secret" mapstructure:"secret"`
	Subscriptions          map[string]*Subscribe `json:"subscriptions" mapstructure:"subscriptions"`
	TransactionCheckPeriod time.Duration         `json:"transactionCheckPeriod" mapstructure:"transactionCheckPeriod"`
	Contracts              map[string]string     `json:"contracts" mapstructure:"contracts"`
	ProcessWithinBlocks    uint64                `json:"processWithinBlocks" mapstructure:"processWithinBlocks"`

	MaxTasksQuery int `json:"maxTasksQuery" mapstructure:"maxTasksQuery"`
	MinTasksQuery int `json:"minTasksQuery" mapstructure:"minTasksQuery"`

	// GetLogsBatchSize is used at batch size when calling processBatchLogs
	GetLogsBatchSize int `json:"getLogsBatchSize" mapstructure:"getLogsBatchSize"`

	// MaxProcessingTasks is used to specify max processing tasks allowed while processing tasks
	// if number of tasks reaches this number, it waits until this number decrease
	MaxProcessingTasks int    `json:"maxProcessingTasks" mapstructure:"maxProcessingTasks"`
	GasLimitBumpRatio  uint64 `json:"gasLimitBumpRatio" mapstructure:"gasLimitBumpRatio"`

	Stats *ListenerStats `json:"stats" mapstructure:"stats"`
}

type ListenerStats struct {
	Node   string `json:"node" mapstructure:"node"`
	Host   string `json:"host" mapstructure:"host"`
	Secret string `json:"secret" mapstructure:"secret"`
}

type Secret struct {
	BridgeOperator       *utils.SignMethodConfig `json:"bridgeOperator" mapstructure:"bridgeOperator"`
	Voter                *utils.SignMethodConfig `json:"voter" mapstructure:"voter"`
	Relayer              *utils.SignMethodConfig `json:"relayer" mapstructure:"relayer"`
	LegacyBridgeOperator *utils.SignMethodConfig `json:"legacyBridgeOperator" mapstructure:"legacyBridgeOperator"`
}

type Subscribe struct {
	From string `json:"from" mapstructure:"from"`
	To   string `json:"to" mapstructure:"to"`

	// Type can be either TxEvent or LogEvent
	Type int `json:"type" mapstructure:"type"`

	Handler   *Handler          `json:"handler" mapstructure:"handler"`
	CallBacks map[string]string `json:"callbacks" mapstructure:"callbacks"`
}

type Handler struct {
	// Contract Name that will be used to get ABI
	Contract string `json:"contract" mapstructure:"contract"`

	// Name is method/event name
	Name string `json:"name" mapstructure:"name"`

	// ContractAddress is used in callback case
	ContractAddress string `json:"contractAddress" mapstructure:"contractAddress"`

	// Listener who triggers callback event
	Listener string `json:"listener" mapstructure:"listener"`

	ABI *abi.ABI `json:"-"`

	// HandleMethod is used when processing listened job, do nothing if it is empty
	HandleMethod string `json:"handleMethod" mapstructure:"handleMethod"`
}

type EmptyTransaction struct {
	chainId     *big.Int
	hash        common.Hash
	from, to    *common.Address
	blockNumber uint64
	data        []byte
}

func NewEmptyTransaction(chainId *big.Int, tx common.Hash, data []byte, from, to *common.Address, blockNumber uint64) *EmptyTransaction {
	return &EmptyTransaction{
		chainId:     chainId,
		hash:        tx,
		from:        from,
		to:          to,
		data:        data,
		blockNumber: blockNumber,
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

func (b *EmptyTransaction) BlockNumber() uint64 {
	return b.blockNumber
}
