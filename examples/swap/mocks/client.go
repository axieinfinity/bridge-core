package mocks

import (
	"context"
	"errors"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/axieinfinity/bridge-core/types"
	"github.com/ethereum/go-ethereum/common"
)

type MockChain struct {
	chainID      *big.Int
	name         string
	miningPeriod time.Duration

	blockNumber       uint64
	transactionNumber uint64
	logNumber         uint64

	blocks []types.Block

	mutexLog     sync.RWMutex
	hash2block   map[common.Hash]types.Block    // block hash -> block
	topic2logs   map[common.Hash][]types.Log    // map topic -> logs
	address2logs map[common.Address][]types.Log // map address -> logs

}

func (c *MockChain) search(blockNumber uint64) types.Block {
	var (
		l = 0
		r = len(c.blocks) - 1
	)

	for l <= r {
		m := l + (r-l)/2
		if c.blocks[m].GetHeight() == uint64(blockNumber) {
			return c.blocks[m]
		} else if c.blocks[m].GetHeight() > uint64(blockNumber) {
			r = m - 1
		} else {
			l = m + 1
		}
	}

	return nil
}

func (c *MockChain) binarySearchLogLower(logs []*types.LogData, blockNumber uint64) int {
	var (
		l = 0
		r = len(logs) - 1
	)

	for l <= r {
		m := l + (r-l)/2
		if logs[m].BlockNumber >= blockNumber {
			r = m - 1
		} else {
			l = m + 1
		}
	}

	return l
}

func (c *MockChain) binarySearchLogUpper(logs []*types.LogData, blockNumber uint64) int {
	var (
		l = 0
		r = len(logs) - 1
	)

	for l <= r {
		m := l + (r-l)/2
		if logs[m].BlockNumber > blockNumber {
			r = m - 1
		} else {
			l = m + 1
		}
	}

	return l
}

func (c *MockChain) joinLog(a []types.Log, b []types.Log) []types.Log {
	var (
		la     = len(a)
		lb     = len(b)
		pa     = 0
		pb     = 0
		result = make([]types.Log, 0)
	)

	for pa < la && pb < lb {
		ida := a[pa].GetExtraData()["id"].(uint64)
		idb := b[pb].GetExtraData()["id"].(uint64)
		if ida == idb {
			result = append(result, a[pa])
			pa++
			pb++
		} else if ida < idb {
			pa++
		} else {
			pb++
		}

	}
	return result
}

// GetLatestBlock(ctx context.Context) (Block, error)
func (c *MockChain) GetLatestBlockHeight(ctx context.Context) (uint64, error) {
	return c.blockNumber, nil
}

func (c *MockChain) GetBlock(ctx context.Context, blockNumber uint64) (types.Block, error) {
	block := c.search(blockNumber)
	if block == nil {
		return nil, errors.New("not found")
	}
	return block, nil
}

func (c *MockChain) GetBlockWithLogs(ctx context.Context, height uint64) (types.Block, error) {
	return c.GetBlock(ctx, height)
}

func (c *MockChain) GetLogs(ctx context.Context, opts *types.GetLogsOption) ([]types.Log, error) {
	var (
		from   = opts.From.Int64()
		to     = opts.To.Int64()
		result = make([]types.Log, 0)

		logsA = make([]types.Log, 0)
		logsT = make([]types.Log, 0)
	)

	for from < to {
		logs := c.blocks[from].GetLogs()
		result = append(result, logs...)
		from++
	}

	if len(opts.Addresses) > 0 {
		c.mutexLog.RLock()
		for _, address := range opts.Addresses {
			logsA = append(logsA, c.address2logs[address]...)
		}
		c.mutexLog.RUnlock()
		result = c.joinLog(result, logsA)
	}

	if len(opts.Topics) > 0 {
		c.mutexLog.RLock()
		for _, topic := range opts.Topics[0] {
			logsT = append(logsT, c.topic2logs[topic]...)
		}
		c.mutexLog.RUnlock()
		result = c.joinLog(result, logsT)
	}

	return result, nil
}

func (c *MockChain) GetReceipt(ctx context.Context, hash common.Hash) (types.Receipt, error) {
	panic("not implemented") // TODO: Implement
}

func (c *MockChain) GetName() string {
	return c.name
}

func (c *MockChain) GetChainID() (*big.Int, error) {
	return c.chainID, nil
}

func (c *MockChain) Period() time.Duration {
	return c.miningPeriod
}

func (c *MockChain) GetSafeBlockRange() uint64 {
	return 10
}

func (c *MockChain) Close(ctx context.Context) error {
	return nil
}

func (c *MockChain) GenTransactions() []types.Transaction {
	var (
		result []types.Transaction
	)

	return result
}

func (c *MockChain) GenLogs() []types.Log {
	var (
		result []types.Log
	)

	result = append(result, c.GenLog())

	return result
}

func (c *MockChain) GenLog() types.Log {
	log := &types.LogData{
		Address: common.HexToAddress("2ecb08f87f075b5769fe543d0e52e40140575ea7"),
		Topics:  []common.Hash{common.HexToHash("d78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822")},
		ExtraData: map[string]interface{}{
			"id": c.logNumber,
		},
	}
	c.mutexLog.Lock()

	c.address2logs[log.Address] = append(c.address2logs[log.Address], log)
	c.topic2logs[log.Topics[0]] = append(c.topic2logs[log.Topics[0]], log)
	c.logNumber++

	c.mutexLog.Unlock()
	return log
}

func (c *MockChain) Mine() types.Block {
	hash := RandStringBytesMaskImprSrc(64)
	transactions := c.GenTransactions()
	logs := c.GenLogs()

	block := &types.BlockData{
		BlockNumber:  big.NewInt(0).SetUint64(c.blockNumber),
		Hash:         common.HexToHash(hash),
		Transactions: make([]*types.TransactionData, 0, len(transactions)),
		Logs:         make([]*types.LogData, 0, len(logs)),
		ReceivedAt:   time.Now(),
	}

	for _, v := range transactions {
		t := v.(*types.TransactionData)
		block.Transactions = append(block.Transactions, t)
	}

	for _, v := range logs {
		l := v.(*types.LogData)
		block.Logs = append(block.Logs, l)
	}

	c.blocks = append(c.blocks, block)
	c.hash2block[block.Hash] = block
	c.blockNumber++
	return block
}

func (c *MockChain) Start(ctx context.Context) {
	var (
		ticker = time.NewTicker(c.miningPeriod)
	)

	for {
		select {
		case <-ticker.C:
			c.Mine()
			log.Printf("Mined block %v", c.blockNumber)
		case <-ctx.Done():
			return
		}
	}
}

func NewMockChain(
	chainID *big.Int,
	name string,
	miningPeriod time.Duration,
) *MockChain {
	return &MockChain{
		chainID:      chainID,
		name:         name,
		miningPeriod: miningPeriod,

		hash2block:   make(map[common.Hash]types.Block),
		topic2logs:   make(map[common.Hash][]types.Log),
		address2logs: make(map[common.Address][]types.Log),
	}
}
