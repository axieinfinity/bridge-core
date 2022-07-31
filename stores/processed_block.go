package stores

import (
	"github.com/axieinfinity/bridge-core/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type processedBlockStore struct {
	*gorm.DB
}

func NewProcessedBlockStore(db *gorm.DB) *processedBlockStore {
	return &processedBlockStore{db}
}

func (b *processedBlockStore) Save(chainId string, height int64) error {
	return b.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&models.ProcessedBlock{Block: height, ChainId: chainId}).Error
}

func (b *processedBlockStore) GetLatestBlock(chainId string) (int64, error) {
	var block *models.ProcessedBlock
	if err := b.Model(&models.ProcessedBlock{}).Where("id = ?", chainId).First(&block).Error; err != nil {
		return -1, err
	}
	return block.Block, nil
}
