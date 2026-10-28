package settings

import (
	"context"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/settings/record"
	"github.com/QuantumNous/new-api/internal/settings/domain"

	"gorm.io/gorm"
)

type Store struct{ db func() *gorm.DB }

func NewStore(db func() *gorm.DB) Store { return Store{db: db} }
func (s Store) All() ([]domain.Option, error) {
	var rows []*record.Option
	err := s.db().Find(&rows).Error
	var options []domain.Option
	for _, row := range rows {
		options = append(options, domain.Option{Key: row.Key, Value: row.Value})
	}
	return options, err
}

// SaveLegacy preserves the ignored FirstOrCreate and Save errors (TECH_DEBT #58).
func (s Store) SaveLegacy(key, value string) {
	option := record.Option{Key: key}
	s.db().FirstOrCreate(&option, record.Option{Key: key})
	option.Value = value
	s.db().Save(&option)
}
func (s Store) SaveBulk(ctx context.Context, keys []string, values map[string]string) error {
	return s.db().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, key := range keys {
			option := record.Option{Key: key}
			if err := tx.FirstOrCreate(&option, record.Option{Key: key}).Error; err != nil {
				return err
			}
			option.Value = values[key]
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
