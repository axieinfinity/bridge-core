package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	internal "github.com/axieinfinity/bridge-core"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/utils"
	migration "github.com/axieinfinity/bridge-migrations"
	"github.com/ethereum/go-ethereum/log"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func NewBridgeController(cfg *internal.Config, db *gorm.DB, helpers utils.Utils) (*internal.Controller, error) {
	internal.AddListener("Ethereum", InitDeposited)
	internal.AddListener("Ronin", InitWithdraw)
	controller, err := internal.New(cfg, db, helpers)
	if err != nil {
		return nil, err
	}
	return controller, nil
}

func InitDeposited(ctx context.Context, lsConfig *internal.LsConfig, store stores.MainStore, helpers utils.Utils) internal.Listener {
	ethListener, err := NewEthereumListener(ctx, lsConfig, helpers, store)
	if err != nil {
		log.Error("[EthereumListener]Error while init new ethereum listener", "err", err.Error())
		return nil
	}

	return ethListener
}

func InitWithdraw(ctx context.Context, lsConfig *internal.LsConfig, store stores.MainStore, helpers utils.Utils) internal.Listener {
	ethListener, err := NewEthereumListener(ctx, lsConfig, helpers, store)
	if err != nil {
		log.Error("[EthereumListener]Error while init new ethereum listener", "err", err.Error())
		return nil
	}

	return ethListener
}

func main() {
	config := &internal.Config{
		Listeners: map[string]*internal.LsConfig{
			"Ethereum": {
				ChainId: "0x03",
				Name:    "Ethereum",
				RpcUrl:  "url",
				Subscriptions: map[string]*internal.Subscribe{
					"WithdrewSubscription": {
						To:   "0x4E4D9B21B157CCD52b978C3a3BCd1dc5eBAE7167",
						Type: 1, // 0 for listening, 1 for callback
						CallBacks: map[string]string{
							"Ethereum": "WithdrewCallback", // Key: Value is Chain name: method name
						},
						Handler: &internal.Handler{
							Contract: "EthereumGateway", // contract name
							Name:     "Withdrew",        // Event name
						},
					},
				},
			},
			"Ronin": {
				ChainId: "0x7e5",
				Name:    "Ronin",
				RpcUrl:  "url",
				Subscriptions: map[string]*internal.Subscribe{
					"DepositedCallback": {
						To:   "0xA8D61A5427a778be28Bd9bb5990956b33385c738",
						Type: 1, // 0 for listening, 1 for callback
						CallBacks: map[string]string{
							"Ronin": "DepositedCallback", // Key: Value is Chain name: method name
						},
						Handler: &internal.Handler{
							Contract: "RoninGateway", // contract name
							Name:     "Deposited",    // Event name
						},
					},
				},
			},
		},
	}
	connectionString := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable", "localhost", "user", "password", "dbname", 5432)
	db, err := gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	if err := migration.Migrate(db, config); err != nil {
		panic(err)
	}

	controller, err := NewBridgeController(config, db, nil)
	if err != nil {
		panic(err)
	}
	controller.Start()

	defer func() {
		controller.Close()
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc
}
