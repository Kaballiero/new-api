package fx_test

import (
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	state "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/contracttest/fxfixture"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"

	"context"
	"math"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withModelCostFXDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	previous := model.DB
	model.DB = db
	state.ClearForTest()
	t.Cleanup(func() {
		model.DB = previous
		state.ClearForTest()
	})
}

func modelCostFXTestSnapshot(effective, fetched int64, rate float64) domain.ModelCostFXSnapshot {
	return domain.ModelCostFXSnapshot{Source: "test-source", EffectiveAt: effective, FetchedAt: fetched, Rates: map[string]float64{"USD": rate}}
}

func TestModelCostFXPersistenceContracts(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fxfixture.Open(t, dialect)
			withModelCostFXDB(t, db)
			ctx := context.Background()
			first := modelCostFXTestSnapshot(100, 110, 90)
			first.Source = "TEST-SOURCE"
			require.NoError(t, storage.SaveModelCostFX(ctx, first, 0))
			first.Rates["USD"] = 1
			current, err := state.CurrentModelCostFX("test-source")
			require.NoError(t, err)
			assert.Equal(t, int64(1), current.Version)
			assert.Equal(t, 90.0, current.Rates["USD"])
			current.Rates["USD"] = 1
			again, err := state.CurrentModelCostFX("test-source")
			require.NoError(t, err)
			assert.Equal(t, 90.0, again.Rates["USD"])
			failedDB, openErr := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, openErr)
			model.DB = failedDB
			assert.Error(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(101, 111, 91), 1))
			model.DB = db
			assert.Equal(t, 90.0, mustCurrentModelCostFX(t).Rates["USD"])

			assert.ErrorIs(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(101, 111, 91), 0), domain.ErrModelCostFXVersionConflict)
			assert.ErrorIs(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(99, 112, 91), 1), domain.ErrModelCostFXStale)
			assert.ErrorIs(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(100, 110, 91), 1), domain.ErrModelCostFXStale)
			for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
				assert.Error(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(101, 112, invalid), 1))
			}
			var row model.ModelCostFXRow
			require.NoError(t, db.First(&row).Error)
			assert.Equal(t, int64(1), row.Version)

			require.NoError(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(101, 113, 91), 1))
			state.ClearForTest()
			require.NoError(t, storage.LoadModelCostFX(ctx, "test-source"))
			assert.Equal(t, int64(2), mustCurrentModelCostFX(t).Version)
			assert.NoError(t, db.Model(&model.ModelCostFXRow{}).Where("source = ?", "test-source").Update("rates_json", "{").Error)
			assert.Error(t, storage.LoadModelCostFX(ctx, "test-source"))
			assert.Equal(t, 91.0, mustCurrentModelCostFX(t).Rates["USD"])
		})
	}
}

func mustCurrentModelCostFX(t *testing.T) domain.ModelCostFXSnapshot {
	t.Helper()
	snapshot, err := state.CurrentModelCostFX("test-source")
	require.NoError(t, err)
	return snapshot
}

func TestModelCostFXConcurrentCAS(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fxfixture.Open(t, dialect)
			withModelCostFXDB(t, db)
			ctx := context.Background()
			require.NoError(t, storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(100, 100, 90), 0))
			start := make(chan struct{})
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for _, rate := range []float64{91, 92} {
				wg.Add(1)
				go func(rate float64) {
					defer wg.Done()
					<-start
					results <- storage.SaveModelCostFX(ctx, modelCostFXTestSnapshot(101, 101, rate), 1)
				}(rate)
			}
			close(start)
			wg.Wait()
			close(results)
			var successes int
			for err := range results {
				if err == nil {
					successes++
				} else {
					assert.NotErrorIs(t, err, nil)
				}
			}
			assert.Equal(t, 1, successes)
			var row model.ModelCostFXRow
			require.NoError(t, db.First(&row).Error)
			assert.Equal(t, int64(2), row.Version)
			assert.Equal(t, row.RatesJSON, ratesJSONForTest(mustCurrentModelCostFX(t)))
		})
	}
}

func TestModelCostFXRAMNeverRollsBack(t *testing.T) {
	t.Cleanup(func() {
		state.ClearForTest()
	})
	state.ClearForTest()
	newer := modelCostFXTestSnapshot(101, 101, 91)
	newer.Version = 2
	older := modelCostFXTestSnapshot(100, 100, 90)
	older.Version = 1
	state.Publish(newer)
	state.Publish(older)
	assert.Equal(t, 91.0, mustCurrentModelCostFX(t).Rates["USD"])
}

func TestModelCostFXSourcesAreIndependent(t *testing.T) {
	db := fxfixture.Open(t, "sqlite")
	withModelCostFXDB(t, db)
	ctx := context.Background()
	usd := modelCostFXTestSnapshot(100, 100, 90)
	eur := modelCostFXTestSnapshot(100, 100, 95)
	eur.Source = "other-source"
	require.NoError(t, storage.SaveModelCostFX(ctx, usd, 0))
	require.NoError(t, storage.SaveModelCostFX(ctx, eur, 0))
	assert.Equal(t, 90.0, mustCurrentModelCostFX(t).Rates["USD"])
	other, err := state.CurrentModelCostFX("other-source")
	require.NoError(t, err)
	assert.Equal(t, 95.0, other.Rates["USD"])
	require.NoError(t, storage.LoadModelCostFX(ctx, "other-source"))
	reloaded, err := state.CurrentModelCostFX("other-source")
	require.NoError(t, err)
	assert.Equal(t, 95.0, reloaded.Rates["USD"])
}

func TestModelCostFXConcurrentFirstCreate(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fxfixture.Open(t, dialect)
			withModelCostFXDB(t, db)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, rate := range []float64{90, 91} {
				go func(rate float64) {
					<-start
					results <- storage.SaveModelCostFX(context.Background(), modelCostFXTestSnapshot(100, 100, rate), 0)
				}(rate)
			}
			close(start)
			first, second := <-results, <-results
			assert.NotEqual(t, first == nil, second == nil)
			var rows []model.ModelCostFXRow
			require.NoError(t, db.Find(&rows).Error)
			assert.Len(t, rows, 1)
			assert.Equal(t, int64(1), rows[0].Version)
		})
	}
}

func ratesJSONForTest(snapshot domain.ModelCostFXSnapshot) string {
	encoded, _ := common.Marshal(snapshot.Rates)
	return string(encoded)
}
