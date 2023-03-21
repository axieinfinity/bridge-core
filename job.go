package bridge_core

import (
	"fmt"
	"math/big"
	"time"

	"github.com/axieinfinity/bridge-core/models"
	"github.com/axieinfinity/bridge-core/utils"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type Job struct {
	ID         int32
	Type       int
	Message    []interface{}
	RetryCount int32
	NextTry    int32
	MaxTry     int32
	BackOff    int32
	Listener   Listener
}

func (job Job) Hash() common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("j-%d-%d-%d", job.ID, job.RetryCount, job.NextTry)))
}

type BaseJob struct {
	utilsWrapper utils.Utils

	id      int32
	jobType int

	retryCount int
	maxTry     int
	nextTry    int64
	backOff    int

	data []byte
	tx   Transaction

	subscriptionName string
	listener         Listener

	fromChainID *big.Int
	createdAt   time.Time
}

func NewBaseJob(listener Listener, job *models.Job, transaction Transaction) (*BaseJob, error) {
	chainId, err := hexutil.DecodeBig(job.FromChainId)
	if err != nil {
		return nil, err
	}
	return &BaseJob{
		jobType:          job.Type,
		retryCount:       job.RetryCount,
		maxTry:           20,
		nextTry:          time.Now().Unix() + int64(job.RetryCount*5),
		backOff:          5,
		data:             common.Hex2Bytes(job.Data),
		tx:               transaction,
		subscriptionName: job.SubscriptionName,
		listener:         listener,
		utilsWrapper:     utils.NewUtils(),
		fromChainID:      chainId,
		id:               int32(job.ID),
		createdAt:        time.Unix(job.CreatedAt, 0),
	}, nil
}

func (e *BaseJob) FromChainID() *big.Int {
	return e.fromChainID
}

func (e *BaseJob) GetID() int32 {
	return e.id
}

func (e *BaseJob) GetType() int {
	return e.jobType
}

func (e *BaseJob) GetRetryCount() int {
	return e.retryCount
}

func (e *BaseJob) GetNextTry() int64 {
	return e.nextTry
}

func (e *BaseJob) GetMaxTry() int {
	return e.maxTry
}

func (e *BaseJob) GetData() []byte {
	return e.data
}

func (e *BaseJob) GetValue() *big.Int {
	return e.tx.GetValue()
}

func (e *BaseJob) GetBackOff() int {
	return e.backOff
}

func (e *BaseJob) Process() ([]byte, error) {
	// save event data to database
	if e.jobType == ListenHandler {
		subscription, ok := e.listener.GetSubscriptions()[e.subscriptionName]
		if ok {
			if err := e.listener.GetStore().GetEventStore().Save(&models.Event{
				EventName:       subscription.Handler.Name,
				TransactionHash: e.tx.GetHash().Hex(),
				FromChainId:     hexutil.EncodeBig(e.FromChainID()),
				CreatedAt:       time.Now().Unix(),
			}); err != nil {
				log.Error(fmt.Sprintf("[%sListenJob][Process] error while storing event to database", e.listener.GetName()), "err", err)
			}
		}
		return e.data, nil
	}
	return nil, nil
}

func (e *BaseJob) String() string {
	return fmt.Sprintf("{Type:%v, Subscription:%v, RetryCount: %v}", e.GetType(), e.GetSubscriptionName(), e.GetRetryCount())
}

func (e *BaseJob) Hash() common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("j-%d-%d-%d", e.id, e.retryCount, e.nextTry)))
}

func (e *BaseJob) IncreaseRetryCount() {
	e.retryCount++
}
func (e *BaseJob) UpdateNextTry(nextTry int64) {
	e.nextTry = nextTry
}

func (e *BaseJob) GetListener() Listener {
	return e.listener
}

func (e *BaseJob) GetSubscriptionName() string {
	return e.subscriptionName
}

func (e *BaseJob) GetTransaction() Transaction {
	return e.tx
}

func (e *BaseJob) Utils() utils.Utils {
	return e.utilsWrapper
}

func (e *BaseJob) Save(status string) error {
	job := &models.Job{
		Listener:         e.listener.GetName(),
		SubscriptionName: e.subscriptionName,
		Type:             e.jobType,
		RetryCount:       e.retryCount,
		Status:           status,
		Data:             common.Bytes2Hex(e.data),
		Transaction:      e.tx.GetHash().Hex(),
		CreatedAt:        time.Now().Unix(),
		FromChainId:      hexutil.EncodeBig(e.fromChainID),
	}
	if e.id > 0 {
		job.ID = int(e.id)
	}
	if err := e.listener.GetStore().GetJobStore().Save(job); err != nil {
		return err
	}
	e.id = int32(job.ID)
	e.createdAt = time.Unix(job.CreatedAt, 0)
	return nil
}

func (e *BaseJob) SetID(id int32) {
	e.id = id
}

func (e *BaseJob) CreatedAt() time.Time {
	return e.createdAt
}
