# Bridge-core
Bridge core is an event fetching library. This library automatically fetch logs from chain then invoke the perspective events. Callback functions will be provided in application side.
## How to create an applicationn
### Preparation

Firstly, we need to clone ronin for go-ethereum replacement later.
```
git clone https://github.com/axieinfinity/ronin.git
```

Create a `go` application
```
mkdir app
cd app
go mod init app
```

Inside `go.mod`, we add this line. By doing this, we replace go-ethereum module by ronin module which cloned from above

```
replace github.com/ethereum/go-ethereum => ../ronin
```

Get `migration` package. We need it for migration.
```
go get -v github.com/axieinfinity/bridge-migrations
```

Create `main.go`, we need to migrate to a postgres database by using `gorm`
```go
import (
	migration "github.com/axieinfinity/bridge-migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)


func main() {
    connectionString := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable", "localhost", "user", "password", "dbname", 5432)
	db, err := gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	if err := migration.Migrate(db, config); err != nil {
		panic(err)
	}
}
	
```

### Implementation
First, we need to provide an implementation of `Listener` interface. Then, we add methods determining event callbacks to the implementation struct. Type of callback method
```go
type Callback func(fromChainId *big.Int, tx bridgeCore.Transaction, data []byte) error
```

For delegating a callback on event, we implement callback methods following the above type
```go
func (l *ConreteListener) WithdrewCallback(fromChainId *big.Int, tx bridge_core.Transaction, data []byte) error {
	// implementation here
}
```

Next, we create an instance of controller. Before creating, we need to call `AddListener` method which receives `ChainName` and an initalizing function returning an instance of `Listener`.

Type of init function:
```go
type Init func(ctx context.Context, lsConfig *bridge_core.LsConfig, store stores.MainStore, helpers utils.Utils) bridge_core.Listener
```

```go
func CreateController(cfg *bridge_core.Config, db *gorm.DB) *bridge_core.Controller {
	bridge_core.AddListener("Ethereum", InitEthereum)
	bridge_core.AddListener("Ronin", InitRonin)
	controller, err := bridge_core.New(cfg, db, nil)
	if err != nil {
		panic(err)
	}
	return controller
}

func InitEthereum(ctx context.Context, lsConfig *bridge_core.LsConfig, store stores.MainStore, helpers utils.Utils) bridge_core.Listener {
	// implementation here
}

func InitRonin(ctx context.Context, lsConfig *bridge_core.LsConfig, store stores.MainStore, helpers utils.Utils) bridge_core.Listener {
	// implementation here
}
```

### Configuration
we need a configuration:
```go
	config := &bridge_core.Config{
		Listeners: map[string]*bridge_core.LsConfig{
			"Ethereum": {
				ChainId: "0x03",
				Name:    "Ethereum",
				RpcUrl:  "url",
				Subscriptions: map[string]*bridge_core.Subscribe{
					"WithdrewSubscription": {
						To:   "0x4E4D9B21B157CCD52b978C3a3BCd1dc5eBAE7167",
						Type: 1, // 0 for listening, 1 for callback
						CallBacks: map[string]string{
							"Ethereum": "WithdrewCallback", // Key: Value is Chain name: method name
						},
						Handler: &bridge_core.Handler{
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
				Subscriptions: map[string]*bridge_core.Subscribe{
					"DepositedCallback": {
						To:   "0xA8D61A5427a778be28Bd9bb5990956b33385c738",
						Type: 1, // 0 for listening, 1 for callback
						CallBacks: map[string]string{
							"Ronin": "DepositedCallback", // Key: Value is Chain name: method name
						},
						Handler: &bridge_core.Handler{
							Contract: "RoninGateway", // contract name
							Name:     "Deposited",    // Event name
						},
					},
				},
			},
		},
	}
```

Config includes a map representing the configuration on this chain. This configuration includes chain id, a map of subscription provides information about event listener and callback, these information are provided inside `Handler` and `Callbacks` perspectively.

## Examples
See [Example](examples/) for sample use cases.
