package stores

import (
	"github.com/axieinfinity/bridge-core/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type eventStore struct {
	*gorm.DB
}

func NewEventStore(db *gorm.DB) *eventStore {
	return &eventStore{db}
}

func (e *eventStore) Save(event *models.Event) error {
	return e.Clauses(clause.OnConflict{DoNothing: true}).Create(event).Error
}

func (e *eventStore) DeleteEvents(createdAt uint64) error {
	return e.Where("created_at <= ?", createdAt).Delete(&models.Event{}).Error
}

func (e *eventStore) Count() int64 {
	var count int64
	e.Model(&models.Event{}).Select("id").Count(&count)
	return count
}
