package fx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/record"
	state "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

// LoadModelCostFX loads the durable publication and publishes it to RAM only
// after all fields and rates have been validated.
func LoadModelCostFX(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if model.DB == nil {
		return errors.New("model cost FX database is unavailable")
	}
	var row record.ModelCostFXRow
	if err := model.DB.WithContext(ctx).Where("source = ?", strings.ToLower(strings.TrimSpace(source))).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrModelCostFXUnavailable
		}
		return fmt.Errorf("load model cost FX snapshot: %w", err)
	}
	var rates map[string]float64
	if err := common.UnmarshalJsonStr(row.RatesJSON, &rates); err != nil {
		return fmt.Errorf("decode model cost FX rates: %w", err)
	}
	snapshot, err := domain.NormalizeModelCostFXSnapshot(domain.ModelCostFXSnapshot{
		Source: row.Source, EffectiveAt: row.EffectiveAt, FetchedAt: row.FetchedAt,
		Version: row.Version, Rates: rates,
	})
	if err != nil {
		return fmt.Errorf("validate model cost FX snapshot: %w", err)
	}
	state.Publish(snapshot)
	return nil
}

// SaveModelCostFX conditionally writes one current publication. expectedVersion
// is zero for first creation; successful updates advance the stored version by
// one. RAM is changed only after the transaction commits.
func SaveModelCostFX(ctx context.Context, snapshot domain.ModelCostFXSnapshot, expectedVersion int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expectedVersion < 0 {
		return domain.ErrModelCostFXVersionConflict
	}
	validated, err := domain.NormalizeModelCostFXSnapshot(snapshot)
	if err != nil {
		return err
	}
	if model.DB == nil {
		return errors.New("model cost FX database is unavailable")
	}

	encoded, err := common.Marshal(validated.Rates)
	if err != nil {
		return fmt.Errorf("encode model cost FX rates: %w", err)
	}
	err = model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row record.ModelCostFXRow
		query := locking.LockForUpdate(tx).Where("source = ?", validated.Source).First(&row)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			if expectedVersion != 0 {
				return domain.ErrModelCostFXVersionConflict
			}
			validated.Version = 1
			if createErr := tx.Create(&record.ModelCostFXRow{Source: validated.Source, EffectiveAt: validated.EffectiveAt, FetchedAt: validated.FetchedAt, Version: validated.Version, RatesJSON: string(encoded)}).Error; createErr != nil {
				return createErr
			}
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		if row.Version != expectedVersion {
			return domain.ErrModelCostFXVersionConflict
		}
		if validated.EffectiveAt < row.EffectiveAt || (validated.EffectiveAt == row.EffectiveAt && validated.FetchedAt <= row.FetchedAt) {
			return domain.ErrModelCostFXStale
		}
		if row.Version == math.MaxInt64 {
			return errors.New("model cost FX version exhausted")
		}
		validated.Version = row.Version + 1
		result := tx.Model(&record.ModelCostFXRow{}).Where("source = ? AND version = ?", validated.Source, expectedVersion).Updates(map[string]any{
			"effective_at": validated.EffectiveAt, "fetched_at": validated.FetchedAt,
			"version": validated.Version, "rates_json": string(encoded),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domain.ErrModelCostFXVersionConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	state.Publish(validated)
	return nil
}
