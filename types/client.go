package types

import (
	"context"
	"math/big"
	"time"

	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type GetLogsOption struct {
	BlockHash *common.Hash
	From      *big.Int
	To        *big.Int
	Addresses []common.Address
	Topics    [][]common.Hash
}

type ChainClient interface {
	// GetLatestBlock(ctx context.Context) (Block, error)
	GetLatestBlockHeight(ctx context.Context) (uint64, error)
	GetBlock(ctx context.Context, blockNumber uint64) (Block, error)
	GetBlockWithLogs(ctx context.Context, height uint64) (Block, error)
	GetLogs(ctx context.Context, opts *GetLogsOption) ([]Log, error)
	GetReceipt(ctx context.Context, hash common.Hash) (Receipt, error)

	GetName() string
	GetChainID() (*big.Int, error)
	Period() time.Duration
	GetSafeBlockRange() uint64
	// GetCurrentBlockHeight() uint64

	Close(ctx context.Context) error
}

type ethClient struct {
	client utils.EthClient

	safeBlockRange uint64

	// currentBlock atomic.Value

	chainID *big.Int

	name string
}

// func (c *ethClient) GetNextBlock(ctx context.Context) (Block, error) {
// 	c.currentHeight++
// 	block, err := c.GetBlock(ctx, c.currentHeight)
// 	if err != nil {
// 		return nil, err
// 	}

// 	c.currentBlock.Store(block)
// 	return block, nil
// }

// func (c *ethClient) GetCurrentBlock(ctx context.Context) (Block, error) {
// 	if v, ok := c.currentBlock.Load().(Block); ok {
// 		return v, nil
// 	}

// 	return c.GetBlock(ctx, c.currentHeight)
// }

func (c *ethClient) GetLatestBlockHeight(ctx context.Context) (uint64, error) {
	return c.client.BlockNumber(ctx)
}

func (c *ethClient) GetBlock(ctx context.Context, blockNumber uint64) (Block, error) {
	return c.getBlock(ctx, blockNumber, false)
}

func (c *ethClient) GetBlockWithLogs(ctx context.Context, blockNumber uint64) (Block, error) {
	return c.getBlock(ctx, blockNumber, true)
}

func (c *ethClient) getBlock(ctx context.Context, blockNumber uint64, getLog bool) (Block, error) {
	block, err := c.client.BlockByNumber(ctx, big.NewInt(int64(blockNumber)))
	if err != nil {
		return nil, err
	}

	transactions := block.Transactions()

	transactionsData := make([]*TransactionData, 0, len(transactions))
	for _, transaction := range transactions {
		transactionsData = append(transactionsData, &TransactionData{
			ChainId: transaction.ChainId(),
			Hash:    transaction.Hash(),
			To:      transaction.To(),
			Data:    transaction.Data(),
		})
	}

	logsData := make([]*LogData, 0)
	if getLog {
		blockHash := block.Hash()

		logs, err := c.client.FilterLogs(context.Background(), ethereum.FilterQuery{BlockHash: &blockHash})
		if err != nil {
			log.Error("[NewEthBlock] error while getting logs", "err", err, "block", block.NumberU64(), "hash", block.Hash().Hex())
			return nil, err
		}
		// convert logs to ILog
		for _, l := range logs {
			logsData = append(logsData, &LogData{
				Address:     l.Address,
				Topics:      l.Topics,
				Data:        l.Data,
				BlockNumber: l.BlockNumber,
				TxHash:      l.TxHash,
				TxIndex:     l.TxIndex,
				BlockHash:   l.BlockHash,
				Index:       l.Index,
				Removed:     l.Removed,
			})
		}
	}

	return &BlockData{
		BlockNumber:  block.Number(),
		Hash:         block.Hash(),
		Transactions: transactionsData,
		Logs:         logsData,
		ReceivedAt:   block.ReceivedAt,
	}, nil
}

func (c *ethClient) GetLatestBlock(ctx context.Context) (Block, error) {
	blockNumber, err := c.GetLatestBlockHeight(ctx)
	if err != nil {
		return nil, err
	}
	return c.getBlock(ctx, blockNumber, false)
}

// func (e *ethClient) GetProcessedBlock(ctx context.Context) (Block, error) {
// 	chainId, err := e.GetChainID()
// 	if err != nil {
// 		log.Error(fmt.Sprintf("[%sListener][GetLatestBlock] error while getting chainId", e.name), "err", err.Error())
// 		return nil, err
// 	}
// 	encodedChainId := hexutil.EncodeBig(chainId)
// 	height, err := e.store.GetProcessedBlockStore().GetLatestBlock(encodedChainId)
// 	if err != nil {
// 		log.Error(fmt.Sprintf("[%sListener][GetLatestBlock] error while getting latest height from database", e.name), "err", err, "chainId", encodedChainId)
// 		return nil, err
// 	}
// 	return e.getBlock(ctx, uint64(height), false)
// }

func (e *ethClient) GetReceipt(ctx context.Context, hash common.Hash) (Receipt, error) {
	receipt, err := e.client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, err
	}

	data := &ReceiptData{}

	data.Transaction = &TransactionData{
		ChainId: e.chainID,
		Hash:    receipt.TxHash,
	}

	data.Logs = make([]Log, 0, len(receipt.Logs))
	for _, l := range receipt.Logs {
		data.Logs = append(data.Logs, &LogData{
			Address:     l.Address,
			Topics:      l.Topics,
			Data:        l.Data,
			BlockNumber: l.BlockNumber,
			TxHash:      l.TxHash,
			TxIndex:     l.TxIndex,
			BlockHash:   l.BlockHash,
			Index:       l.Index,
			Removed:     l.Removed,
		})
	}

	data.Status = receipt.Status > 0
	return data, nil
}

func (e *ethClient) GetLogs(ctx context.Context, opts *GetLogsOption) ([]Log, error) {
	logs, err := e.client.FilterLogs(ctx, ethereum.FilterQuery{
		BlockHash: opts.BlockHash,
		Addresses: opts.Addresses,
		Topics:    opts.Topics,
		FromBlock: opts.From,
		ToBlock:   opts.To,
	})
	if err != nil {
		return nil, err
	}

	result := make([]Log, 0, len(logs))
	for _, l := range logs {
		result = append(result, &LogData{
			Address:     l.Address,
			Topics:      l.Topics,
			Data:        l.Data,
			BlockNumber: l.BlockNumber,
			TxHash:      l.TxHash,
			TxIndex:     l.TxIndex,
			BlockHash:   l.BlockHash,
			Index:       l.Index,
			Removed:     l.Removed,
		})
	}

	return result, nil
}

func (e *ethClient) GetChainID() (*big.Int, error) {
	if e.chainID == nil {
		chainID, err := e.client.ChainID(context.Background())
		if err != nil {
			return nil, err
		}
		e.chainID = chainID
	}
	return e.chainID, nil
}

func (e *ethClient) GetName() string {
	return e.name
}

func (e *ethClient) Period() time.Duration {
	return time.Second
}

func (c *ethClient) GetSafeBlockRange() uint64 {
	return c.safeBlockRange
}

// func (c *ethClient) GetCurrentBlockHeight() uint64 {
// 	return c.currentHeight
// }

func (e *ethClient) Close(ctx context.Context) error {
	e.client.Close()
	return nil
}

func NewEthClient(client utils.EthClient, name string, safeBlockRange uint64) ChainClient {
	return &ethClient{
		client:         client,
		safeBlockRange: safeBlockRange,
		name:           name,
	}
}
