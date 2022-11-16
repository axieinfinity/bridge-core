package main

import (
	"math/big"

	internal "github.com/axieinfinity/bridge-core"
	"github.com/ethereum/go-ethereum/log"
)

type DepositedListener struct {
	*EthereumListener
}

func (l *DepositedListener) DepositedCallback(fromChainId *big.Int, tx internal.Transaction, data []byte) error {
	log.Info("DepositedListener", "tx", tx.GetHash().Hex())
	return nil
}

func NewDepositedListener(l *EthereumListener) *DepositedListener {
	return &DepositedListener{
		EthereumListener: l,
	}
}
