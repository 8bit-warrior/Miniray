package service

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

// AddTuicTrafficBatch commits a journal batch and its receipt atomically. A
// retained journal can therefore be replayed after an ambiguous commit result.
func (s *InboundService) AddTuicTrafficBatch(id string, traffic []*xray.ClientTraffic) error {
	if id == "" {
		return errors.New("TUIC traffic batch has no ID")
	}
	return submitTrafficWrite(func() error {
		return database.GetDB().Transaction(func(tx *gorm.DB) error {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.TuicTrafficReceipt{ID: id})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}
			return s.addClientTraffic(tx, traffic)
		})
	})
}
