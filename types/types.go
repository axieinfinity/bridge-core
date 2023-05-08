package types

import (
	"math/big"
	"time"

	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const (
	ListenHandler = iota
	CallbackHandler
)

const (
	JobTypeTransaction = iota
	JobTypeLog
)

type Transaction interface {
	GetHash() common.Hash
	GetFromAddress() string
	GetToAddress() string
	GetData() []byte
	GetValue() *big.Int
	GetChainID() *big.Int
}

type TransactionData struct {
	ChainId *big.Int `json:"chain_id"`

	Hash common.Hash     `json:"hash,omitempty"`
	From *common.Address `json:"from,omitempty"`
	To   *common.Address `json:"to,omitempty"`
	Data []byte
}

func (t *TransactionData) GetChainID() *big.Int {
	return t.ChainId
}

func (t *TransactionData) GetHash() common.Hash {
	return t.Hash
}

func (t *TransactionData) GetFromAddress() string {
	return t.From.Hex()
}

func (t *TransactionData) GetToAddress() string {
	return t.To.Hex()
}

func (t *TransactionData) GetData() []byte {
	return t.Data
}

func (t *TransactionData) GetValue() *big.Int {
	return nil
}

type Log interface {
	GetChainID() *big.Int
	GetContractAddress() string
	GetTopics() []string
	GetData() []byte
	GetIndex() uint
	GetTxIndex() uint
	GetTransactionHash() string
	GetExtraData() map[string]interface{}
}

type LogData struct {
	ChainId *big.Int `json:"chain_id"`
	// Consensus fields:
	// address of the contract that generated the event
	Address common.Address `json:"address" gencodec:"required"`
	// list of topics provided by the contract.
	Topics []common.Hash `json:"topics" gencodec:"required"`
	// supplied by the contract, usually ABI-encoded
	Data []byte `json:"data" gencodec:"required"`

	// hash of the transaction
	TxHash  common.Hash `json:"transactionHash" gencodec:"required"`
	TxIndex uint
	// Derived fields. These fields are filled in by the node
	// but not secured by consensus.
	// block in which the transaction was included
	BlockNumber uint64 `json:"blockNumber"`

	// hash of the block in which the transaction was included
	BlockHash common.Hash `json:"blockHash"`
	// index of the log in the block
	Index uint `json:"logIndex"`

	// The Removed field is true if this log was reverted due to a chain reorganisation.
	// You must pay attention to this field if you receive logs through a filter query.
	Removed bool `json:"removed"`

	ExtraData map[string]interface{} `json:"custom"`
}

func (l *LogData) GetChainID() *big.Int {
	return l.ChainId
}

func (l *LogData) GetContractAddress() string {
	return l.Address.Hex()
}

func (l *LogData) GetTopics() []string {
	result := make([]string, 0, len(l.Topics))
	for _, v := range l.Topics {
		result = append(result, v.Hex())
	}
	return result
}

func (l *LogData) GetData() []byte {
	return l.Data
}

func (l *LogData) GetIndex() uint {
	return l.Index
}

func (l *LogData) GetTxIndex() uint {
	return l.TxIndex
}

func (l *LogData) GetTransactionHash() string {
	return l.TxHash.Hex()
}

func (l *LogData) GetExtraData() map[string]interface{} {
	return l.ExtraData
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

type TaskHandler interface {
	Start()
	Close()
	GetListener() Listener
	SetLimitQuery(limit int)
}

type Config struct {
	Listeners       map[string]*LsConfig `json:"listeners"`
	NumberOfWorkers int                  `json:"numberOfWorkers"`
	MaxQueueSize    int                  `json:"maxQueueSize"`
	MaxRetry        int32                `json:"maxRetry"`
	BackOff         int32                `json:"backoff"`

	// this field is used for testing purpose
	Testing bool
}

type LsConfig struct {
	ChainId        string        `json:"chainId"`
	Name           string        `json:"-"`
	RpcUrl         string        `json:"rpcUrl"`
	LoadInterval   time.Duration `json:"blockTime"`
	SafeBlockRange uint64        `json:"safeBlockRange"`
	FromHeight     uint64        `json:"fromHeight"`
	TaskInterval   time.Duration `json:"taskInterval"`
	Disabled       bool          `json:"disabled"`

	// TODO: apply more ways to get privatekey. such as: PLAINTEXT, KMS, etc.
	Secret                 *Secret               `json:"secret"`
	Subscriptions          map[string]*Subscribe `json:"subscriptions"`
	TransactionCheckPeriod time.Duration         `json:"transactionCheckPeriod"`
	Contracts              map[string]string     `json:"contracts"`
	ProcessWithinBlocks    uint64                `json:"processWithinBlocks"`

	MaxTasksQuery int `json:"maxTasksQuery"`
	MinTasksQuery int `json:"minTasksQuery"`

	// GetLogsBatchSize is used at batch size when calling processBatchLogs
	GetLogsBatchSize int `json:"getLogsBatchSize"`

	// MaxProcessingTasks is used to specify max processing tasks allowed while processing tasks
	// if number of tasks reaches this number, it waits until this number decrease
	MaxProcessingTasks int `json:"maxProcessingTasks"`
}

type Secret struct {
	BridgeOperator       *utils.SignMethodConfig `json:"bridgeOperator"`
	Voter                *utils.SignMethodConfig `json:"voter"`
	Relayer              *utils.SignMethodConfig `json:"relayer"`
	LegacyBridgeOperator *utils.SignMethodConfig `json:"legacyBridgeOperator"`
}

type Subscribe struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Type can be either TxEvent or LogEvent
	Type int `json:"type"`

	Handler   *Handler          `json:"handler"`
	CallBacks map[string]string `json:"callbacks"`
}

type Handler struct {
	// Contract Name that will be used to get ABI
	Contract string `json:"contract"`

	// Name is method/event name
	Name string `json:"name"`

	// ContractAddress is used in callback case
	ContractAddress string `json:"contractAddress"`

	// Listener who triggers callback event
	Listener string `json:"listener"`

	ABI *abi.ABI `json:"-"`

	// HandleMethod is used when processing listened job, do nothing if it is empty
	HandleMethod string `json:"handleMethod"`
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

// type CallbackData struct {
// 	ChainId *big.Int `json:"chain_id"`

// 	TransactionData *TransactionData `json:"transaction_data,omitempty"`
// 	LogData         *LogData         `json:"log_data,omitempty"`
// }

type BlockData struct {
	BlockNumber *big.Int

	Hash common.Hash

	Transactions []*TransactionData
	Logs         []*LogData
	ReceivedAt   time.Time
}

func (b *BlockData) GetHash() common.Hash {
	return b.Hash
}

func (b *BlockData) GetHeight() uint64 {
	return b.BlockNumber.Uint64()
}

func (b *BlockData) GetTransactions() []Transaction {
	var (
		txn = make([]Transaction, 0, len(b.Transactions))
	)

	for _, v := range b.Transactions {
		txn = append(txn, v)
	}

	return txn
}

func (b *BlockData) GetLogs() []Log {
	var (
		logs = make([]Log, 0, len(b.Logs))
	)
	for _, v := range b.Logs {
		logs = append(logs, v)
	}
	return logs
}

func (b *BlockData) GetTimestamp() uint64 {
	return uint64(b.ReceivedAt.Unix())
}

type ReceiptData struct {
	Transaction Transaction
	Status      bool
	Logs        []Log
}

func (r *ReceiptData) GetTransaction() Transaction {
	return r.Transaction
}

func (r *ReceiptData) GetStatus() bool {
	return r.Status
}

func (r *ReceiptData) GetLogs() []Log {
	return r.Logs
}
