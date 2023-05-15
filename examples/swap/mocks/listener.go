package mocks

import (
	"context"

	"github.com/axieinfinity/bridge-core/types"
	"github.com/ethereum/go-ethereum/log"
)

type MockListener struct {
}

func (l *MockListener) GetName() string {
	return "Mock"
}

func (l *MockListener) TriggerLog(ctx context.Context, event string, data types.Log) error {
	log.Info("log is triggered", "event", event, "chain_id", data.GetChainID())
	return nil
}

func (l *MockListener) TriggerTransaction(ctx context.Context, event string, data types.Transaction) error {
	log.Info("transaction is triggered", "event", event, "chain_id", data.GetChainID())
	return nil
}

func NewMockListener() types.Listener {
	return &MockListener{}
}
