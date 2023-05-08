package main

import (
	"context"
	"log"
	"math/big"
	"os"
	"os/signal"
	"time"

	bridge_core "github.com/axieinfinity/bridge-core"
	"github.com/axieinfinity/bridge-core/configs"
	"github.com/axieinfinity/bridge-core/examples/swap/mocks"
	"github.com/axieinfinity/bridge-core/orchestrators"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/axieinfinity/bridge-core/types"
	elog "github.com/ethereum/go-ethereum/log"
)

func main() {
	elog.Root().SetHandler(elog.LvlFilterHandler(elog.LvlInfo, elog.StreamHandler(os.Stderr, elog.TerminalFormat(true))))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)

	db, err := stores.MustConnectDatabase(&stores.Database{
		Host:     configs.AppConfig.DB.Host,
		User:     configs.AppConfig.DB.User,
		Password: configs.AppConfig.DB.Password,
		DBName:   configs.AppConfig.DB.DBName,
		Port:     configs.AppConfig.DB.Port,
	}, false)
	if err != nil {
		panic(err)
	}

	var (
		store              = stores.NewMainStore(db)
		logWorkers         []types.Worker[types.Log]
		transactionWorkers []types.Worker[types.Transaction]
		listeners          = make(map[string]types.Listener)
	)

	for i := 0; i < 1000; i++ {
		logWorkers = append(logWorkers, types.NewLogWorker(i, listeners))
	}
	var (
		clients         = make(map[string]types.ChainClient)
		subscriptions   = make(map[string]*types.Subscribe)
		logPool         = types.NewPool(logWorkers, 1000)
		transactionPool = types.NewPool(transactionWorkers, 1000)
		// util            = utils.NewUtils()
	)

	// eth, err := util.NewEthClient("https://saigon-testnet.roninchain.com/rpc")
	// if err != nil {
	// 	panic(err)
	// }

	mockChain := mocks.NewMockChain(big.NewInt(2023), "Mock", time.Millisecond*10)
	// subscriptions["Ronin"] = &types.Subscribe{
	// 	To:   "fd25f2eb3d5ead58b47cd4af645f1c136d8f0455",
	// 	Type: 1,
	// 	Handler: &types.Handler{
	// 		Contract: "KatanaPair",
	// 		Name:     "Swap",
	// 	},
	// }

	subscriptions["Mock"] = &types.Subscribe{
		To:   "2ecb08f87f075b5769fe543d0e52e40140575ea7",
		Type: 1,
		Handler: &types.Handler{
			Contract: "KatanaPair",
			Name:     "Swap",
		},
	}
	// clients["Ronin"] = types.NewEthClient(eth, "Ronin", 10)
	clients["Mock"] = mockChain
	var (
		logOrchestrator         = orchestrators.NewLogService(logPool, store)
		transactionOrchestrator = orchestrators.NewTransactionOrchestrator(transactionPool, store)
	)

	var (
		controller = bridge_core.New(store, clients, subscriptions, logOrchestrator, transactionOrchestrator)
	)

	if err := controller.LoadABIs(); err != nil {
		panic(err)
	}

	go mockChain.Start(ctx)

	if err := controller.Start(ctx); err != nil {
		panic(err)
	}

	defer func() {
		sqlDB, err := db.DB()
		if err != nil {
			return
		}

		log.Printf("closing")
		_ = sqlDB.Close()

		mockChain.Close(ctx)
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	log.Printf("shutting down")

	cancel()
	controller.Close(ctx)
	// time.Sleep(time.Second * 3)
}
