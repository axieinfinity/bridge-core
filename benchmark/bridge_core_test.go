package benchmark

import (
	"context"
	"fmt"
	bridge_core "github.com/axieinfinity/bridge-core"
	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/stores"
	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

var testData = make([]string, 0)

func init() {
	dataLength, _ := strconv.Atoi(os.Getenv("size"))
	for i := 0; i < dataLength; i++ {
		testData = append(testData, fmt.Sprintf("%v", rand.Uint32()))
	}
}

type Job struct {
	store   stores.MainStore
	id      int32
	jobType int

	retryCount int
	maxTry     int
	nextTry    int64
	backOff    int

	data []byte
}

func NewJob(id int32, store stores.MainStore, data []byte) *Job {
	return &Job{
		id:         id,
		retryCount: 0,
		maxTry:     20,
		backOff:    5,
		store:      store,
		data:       data,
	}
}

func (e *Job) FromChainID() *big.Int {
	return nil
}

func (e *Job) GetID() int32 {
	return e.id
}

func (e *Job) GetType() int {
	return e.jobType
}

func (e *Job) GetRetryCount() int {
	return e.retryCount
}

func (e *Job) GetNextTry() int64 {
	return e.nextTry
}

func (e *Job) GetMaxTry() int {
	return e.maxTry
}

func (e *Job) GetData() []byte {
	return e.data
}

func (e *Job) GetValue() *big.Int {
	return nil
}

func (e *Job) GetBackOff() int {
	return e.backOff
}

func (e *Job) Process() ([]byte, error) {
	return nil, nil
}

func (e *Job) String() string {
	return fmt.Sprintf("{Type:%v, Subscription:%v, RetryCount: %v}", e.GetType(), e.GetSubscriptionName(), e.GetRetryCount())
}

func (e *Job) Hash() common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("j-%d-%d-%d", e.id, e.retryCount, e.nextTry)))
}

func (e *Job) IncreaseRetryCount() {
	e.retryCount++
}
func (e *Job) UpdateNextTry(nextTry int64) {
	e.nextTry = nextTry
}

func (e *Job) GetListener() bridge_core.Listener {
	return nil
}

func (e *Job) GetSubscriptionName() string {
	return ""
}

func (e *Job) GetTransaction() bridge_core.Transaction {
	return nil
}

func (e *Job) Save() error {
	//job := &models.Job{
	//	Listener:         "",
	//	SubscriptionName: e.GetSubscriptionName(),
	//	Type:             e.GetType(),
	//	RetryCount:       e.retryCount,
	//	Status:           stores.STATUS_PENDING,
	//	Data:             common.Bytes2Hex(e.GetData()),
	//	Transaction:      "",
	//	CreatedAt:        time.Now().Unix(),
	//	FromChainId:      "",
	//}
	//if err := e.store.GetJobStore().Save(job); err != nil {
	//	return err
	//}
	//e.id = int32(job.ID)
	return nil
}

func (e *Job) Update(status string) error {
	job := &models.Job{
		Listener:         "",
		SubscriptionName: e.GetSubscriptionName(),
		Type:             e.GetType(),
		RetryCount:       e.retryCount,
		Status:           status,
		Data:             common.Bytes2Hex(e.GetData()),
		Transaction:      "",
		CreatedAt:        time.Now().Unix(),
		FromChainId:      "",
	}
	if err := e.store.GetJobStore().Save(job); err != nil {
		return err
	}
	e.id = int32(job.ID)
	return nil
}

func (e *Job) SetID(id int32) {
	e.id = id
}

func (e *Job) CreatedAt() time.Time {
	return time.Now()
}

func addWorkers(ctx context.Context, pool *bridge_core.Pool, cfg *bridge_core.Config) {
	var workers []bridge_core.Worker
	for i := 0; i < cfg.NumberOfWorkers; i++ {
		workers = append(workers, bridge_core.NewWorker(ctx, i, pool.PrepareJobChan, pool.FailedJobChan, pool.Queue, pool.MaxQueueSize, nil))
	}
	pool.AddWorkers(workers)
}

func newPool(ctx context.Context, db *gorm.DB, numberOfWorkers int) *bridge_core.Pool {
	bridgeCnf := &bridge_core.Config{NumberOfWorkers: numberOfWorkers}
	pool := bridge_core.NewPool(ctx, bridgeCnf, db, nil)
	addWorkers(ctx, pool, bridgeCnf)
	return pool
}

func BenchmarkPool(b *testing.B) {
	dbCfg := &stores.Database{
		Host:            "localhost",
		User:            "postgres",
		Password:        "example",
		DBName:          "bench_mark_db",
		Port:            5432,
		ConnMaxLifetime: 200,
		MaxIdleConns:    200,
		MaxOpenConns:    200,
	}
	// init db based on config
	db, err := stores.MustConnectDatabase(dbCfg, false)
	if err != nil {
		panic(err)
	}
	db.AutoMigrate(&models.Job{})
	ctx, cancel := context.WithCancel(context.Background())
	pool := newPool(ctx, db, 8192)
	go pool.Start(nil)

	store := stores.NewMainStore(db)
	now := time.Now()
	for _, data := range testData {
		pool.PrepareJobChan <- NewJob(0, store, []byte(data))
	}
	for {
		if len(pool.PrepareJobChan) == 0 && len(pool.JobChan) == 0 && len(pool.FailedJobChan) == 0 && len(pool.RetryJobChan) == 0 {
			cancel()
			pool.Wait()
			break
		}
	}
	println(fmt.Sprintf("total time: %d (ms)", time.Now().Sub(now).Milliseconds()))
}
