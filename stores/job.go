package stores

import (
	"fmt"
	"github.com/axieinfinity/bridge-core/models"
	"gorm.io/gorm"
)

type jobStore struct {
	*gorm.DB
}

func NewJobStore(db *gorm.DB) *jobStore {
	return &jobStore{db}
}

func (j *jobStore) Save(job *models.Job) error {
	return j.Create(job).Error
}

func (j *jobStore) Update(job *models.Job) error {
	return j.Model(models.Job{}).Where("id = ?", job.ID).Updates(job).Error
}

func (j *jobStore) GetPendingJobs() ([]*models.Job, error) {
	// query all pending jobs
	var jobs []*models.Job
	err := j.Model(&models.Job{}).Where("status = ?", STATUS_PENDING).
		Order(fmt.Sprintf("created_at + POWER(2, retry_count) * 10 ASC")).Find(&jobs).Error
	return jobs, err
}

func (j *jobStore) DeleteJobs(status []string, createdAt uint64) error {
	return j.Where("status in ? AND created_at <= ?", status, createdAt).Delete(&models.Job{}).Error
}

func (j *jobStore) Count() int64 {
	var count int64
	j.Model(&models.Job{}).Select("id").Count(&count)
	return count
}
