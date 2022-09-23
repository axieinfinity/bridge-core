package stores

import (
	"fmt"
	"time"

	"github.com/axieinfinity/bridge-core/models"
	"github.com/ethereum/go-ethereum/log"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	STATUS_PENDING    = "pending"
	STATUS_FAILED     = "failed"
	STATUS_PROCESSING = "processing"
	STATUS_DONE       = "done"
)

type Database struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbName"`
	Port     int    `json:"port"`

	ConnMaxLifetime int `json:"connMaxLifeTime"`
	MaxIdleConns    int `json:"maxIdleConns"`
	MaxOpenConns    int `json:"maxOpenConns"`
}

type JobStore interface {
	Save(job *models.Job) error
	Update(job *models.Job) error
	GetPendingJobs() ([]*models.Job, error)
	GetPendingJobsByTime(upper int64) ([]*models.Job, error)
	DeleteJobs([]string, uint64) error
	Count() int64
}

type ProcessedBlockStore interface {
	Save(chainId string, height int64) error
	GetLatestBlock(chainId string) (int64, error)
}

type EventStore interface {
	Save(event *models.Event) error
	DeleteEvents(uint64) error
	Count() int64
}

type MainStore interface {
	GetDB() *gorm.DB
	GetJobStore() JobStore
	GetProcessedBlockStore() ProcessedBlockStore
	GetEventStore() EventStore
}

type mainStore struct {
	*gorm.DB

	JobStore            JobStore
	ProcessedBlockStore ProcessedBlockStore
	EventStore          EventStore
}

func NewMainStore(db *gorm.DB) MainStore {
	cl := &mainStore{
		DB: db,

		JobStore:            NewJobStore(db),
		ProcessedBlockStore: NewProcessedBlockStore(db),
		EventStore:          NewEventStore(db),
	}
	return cl
}

func (m *mainStore) RelationalDatabaseCheck() error {
	return m.Raw("SELECT 1").Error
}

func (m *mainStore) GetDB() *gorm.DB {
	return m.DB
}

func (m *mainStore) GetJobStore() JobStore {
	return m.JobStore
}

func (m *mainStore) GetProcessedBlockStore() ProcessedBlockStore {
	return m.ProcessedBlockStore
}

func (m *mainStore) GetEventStore() EventStore {
	return m.EventStore
}

func MustConnectDatabase(dbConfig *Database, testing bool) (*gorm.DB, error) {
	// load sqlite db for testing purpose
	if testing {
		return gorm.Open(sqlite.Open("gorm.db"), &gorm.Config{})
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable", dbConfig.Host, dbConfig.User, dbConfig.Password, dbConfig.DBName, dbConfig.Port)
	dialect := postgres.Open(dsn)
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		panic(err)
	}
	pgDB, err := db.DB()
	if err != nil {
		panic(err)
	}

	pgDB.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Hour)
	pgDB.SetMaxIdleConns(dbConfig.MaxIdleConns)
	pgDB.SetMaxOpenConns(dbConfig.MaxOpenConns)

	err = db.Raw("SELECT 1").Error
	if err != nil {
		log.Error("error querying SELECT 1", "err", err)
		panic(err)
	}
	return db, err
}

func MustConnectDatabaseWithName(dbConfig *Database, dbName string, testing bool) (*gorm.DB, error) {
	var (
		err error
		db  *gorm.DB
	)
	// load sqlite db for testing purpose
	if testing {
		db, err = gorm.Open(sqlite.Open("gorm.db"), &gorm.Config{})
		if err != nil {
			panic(err)
		}
	} else {
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable", dbConfig.Host, dbConfig.User, dbConfig.Password, dbName, dbConfig.Port)
		dialect := postgres.Open(dsn)
		db, err = gorm.Open(dialect, &gorm.Config{})
		if err != nil {
			panic(err)
		}
		pgDB, err := db.DB()
		if err != nil {
			panic(err)
		}

		pgDB.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Hour)
		pgDB.SetMaxIdleConns(dbConfig.MaxIdleConns)
		pgDB.SetMaxOpenConns(dbConfig.MaxOpenConns)
	}

	err = db.Raw("SELECT 1").Error
	if err != nil {
		log.Error("error querying SELECT 1", "err", err)
		panic(err)
	}
	return db, err
}
