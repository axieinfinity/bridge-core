package main

import (
	"math/big"

	bridge_core "github.com/axieinfinity/bridge-core"
	"github.com/ethereum/go-ethereum/log"
)

type WithdrewListener struct {
	*EthereumListener
}

func (l *WithdrewListener) WithdrewCallback(fromChainId *big.Int, tx bridge_core.Transaction, data []byte) error {
	log.Info("WithdrewCallback", "tx", tx.GetHash().Hex())
	return nil
}

func NewWithdrewListener(l *EthereumListener) *WithdrewListener {
	return &WithdrewListener{
		EthereumListener: l,
	}
}
