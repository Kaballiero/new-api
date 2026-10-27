// Package fxseed shares the native gateway FX publication fixture.
package fxseed

import (
	"context"
	"errors"
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func SeedNativeRouteFX(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row model.ModelCostFXRow
	err := db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effectiveAt, fetchedAt := row.EffectiveAt, row.FetchedAt
	currentVersion := int64(0)
	if current, currentErr := fxstate.CurrentModelCostFX("cbr-xml-daily.ru"); currentErr == nil {
		currentVersion = current.Version
		if current.EffectiveAt > effectiveAt {
			effectiveAt = current.EffectiveAt
		}
		if current.FetchedAt > fetchedAt {
			fetchedAt = current.FetchedAt
		}
	}
	for first := true; first || row.Version <= currentVersion; first = false {
		effectiveAt++
		fetchedAt++
		require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{
			Source: "cbr-xml-daily.ru", EffectiveAt: effectiveAt, FetchedAt: fetchedAt,
			Rates: map[string]float64{"USD": 100},
		}, row.Version))
		require.NoError(t, db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error)
	}
	require.NoError(t, fxstorage.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
}
