package types

import (
	"context"
)

type Listener interface {
	GetName() string
	// GetStore() stores.MainStore
	// Config() *LsConfig

	// Period() time.Duration
	// GetSafeBlockRange() uint64
	// GetCurrentBlock() Block
	// GetLatestBlock() (Block, error)
	// GetLatestBlockHeight() (uint64, error)
	// GetBlock(height uint64) (Block, error)
	// GetBlockWithLogs(height uint64) (Block, error)
	// GetChainID() (*big.Int, error)
	// GetReceipt(common.Hash) (*types.Receipt, error)
	// Context() context.Context

	GetSubscriptions() map[string]*Subscribe

	// UpdateCurrentBlock(block Block) error

	// SaveCurrentBlockToDB() error
	// SaveTransactionsToDB(txs []Transaction) error

	// GetListenHandleJob(subscriptionName string, tx Transaction, eventId string, data []byte) Job
	SendCallbackJobs(listeners map[string]Listener, subscriptionName string, tx Transaction, inputData []byte)

	TriggerLog(ctx context.Context, event string, data Log) error
	TriggerTransaction(ctx context.Context, event string, data Transaction) error
	Start()
	Close()

	// IsDisabled() bool
	// SetInitHeight(uint64)
	// GetInitHeight() uint64

	// GetEthClient() utils.EthClient

	// GetTasks() []TaskHandler
	// GetTask(index int) TaskHandler
	// AddTask(handler TaskHandler)

	IsUpTodate() bool

	// GetBridgeOperatorSign() utils.ISign
	// GetVoterSign() utils.ISign
	// GetRelayerSign() utils.ISign
	// GetLegacyBridgeOperatorSign() utils.ISign

	// AddListeners(map[string]Listener)

	// GetListener returns listener by name
	// GetListener(string) Listener
	// CacheBlocks(blockNumbers map[uint64]struct{})
}
