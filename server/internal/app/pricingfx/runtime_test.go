package pricingfx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func withModelCostFXServiceDialect(t *testing.T) {
	t.Helper()
	dialect := os.Getenv("FX_TEST_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	var db *gorm.DB
	var err error
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	ownsDB := false
	// Keep one database per process/dialect so the in-memory FX cache and the
	// optimistic-lock row remain aligned across -count iterations.
	switch dialect {
	case "sqlite":
		db = model.DB
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		require.NotEmpty(t, dsn, "TEST_MYSQL_DSN is required for FX_TEST_DIALECT=mysql")
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
		ownsDB = true
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is required for FX_TEST_DIALECT=postgres")
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		ownsDB = true
	default:
		t.Fatalf("unsupported FX_TEST_DIALECT %q", dialect)
	}
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		if ownsDB {
			sqlDB, closeErr := db.DB()
			if closeErr == nil {
				_ = sqlDB.Close()
			}
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	t.Logf("FX_TEST_DIALECT=%s", dialect)
}

// truncateModelCostFXService only clears the tables migrated by the FX fixture.
func truncateModelCostFXService(t *testing.T) {
	t.Helper()
	db := model.DB
	t.Cleanup(func() {
		for _, table := range []string{"system_task_locks", "system_tasks", "model_cost_fx"} {
			require.NoError(t, db.Exec("DELETE FROM "+table).Error)
		}
	})
}

func TestModelCostFXFixtureKeepsTaskBillingDatabaseIsolated(t *testing.T) {
	taskfixture.WithSQLite(t)
	billingDB, billingLogDB := model.DB, model.LOG_DB
	require.NoError(t, billingDB.Create(&model.Task{TaskID: "fx-fixture-foreign-task", Quota: 47}).Error)
	t.Setenv("FX_TEST_DIALECT", "sqlite")
	t.Run("fx_only", func(t *testing.T) {
		withModelCostFXServiceDialect(t)
		truncateModelCostFXService(t)
		require.Same(t, billingDB, model.DB)
	})
	assert.Same(t, billingDB, model.DB)
	assert.Same(t, billingLogDB, model.LOG_DB)
	var task model.Task
	require.NoError(t, billingDB.Where("task_id = ?", "fx-fixture-foreign-task").First(&task).Error)
	assert.Equal(t, 47, task.Quota)
}

func seedModelCostFXServiceTest(t *testing.T) pricingdomainfx.ModelCostFXSnapshot {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ModelCostFXRow{}))
	// Synchronize the process cache with the dialect-specific row left by an
	// earlier count iteration before deriving the next optimistic-lock version.
	_ = fxstorage.LoadModelCostFX(context.Background(), modelCostFXSource)
	var row model.ModelCostFXRow
	err := model.DB.Where("source = ?", modelCostFXSource).First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effective := row.EffectiveAt + 1
	if effective <= 1 {
		effective = 100
	}
	fetched := time.Now().Unix() - 60
	snapshot := pricingdomainfx.ModelCostFXSnapshot{Source: modelCostFXSource, EffectiveAt: effective, FetchedAt: fetched, Rates: map[string]float64{"USD": 90}}
	require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), snapshot, row.Version))
	loaded, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	return loaded
}

func TestModelCostFXRefreshNormalizesUSDNominal(t *testing.T) {
	withModelCostFXServiceDialect(t)
	truncateModelCostFXService(t)
	seed := seedModelCostFXServiceTest(t)
	publicationDate := time.Unix(seed.EffectiveAt+1, 0).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Date":"%s","Valute":{"USD":{"CharCode":"USD","Value":900,"Nominal":10}}}`, publicationDate)
	}))
	defer server.Close()
	previousEndpoint := modelCostFXEndpoint
	modelCostFXEndpoint = server.URL
	t.Cleanup(func() { modelCostFXEndpoint = previousEndpoint })

	result, err := Refresh().Execute(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Version, int64(1))
	snapshot, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, 90.0, snapshot.Rates["USD"])
	second, err := Refresh().Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, result.Version, second.Version)
	assert.Equal(t, result.FetchedAt, second.FetchedAt)
}

func TestModelCostFXRefreshFailureRetainsLastGood(t *testing.T) {
	withModelCostFXServiceDialect(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ModelCostFXRow{}))
	truncateModelCostFXService(t)
	baselineSnapshot := seedModelCostFXServiceTest(t)
	baselineVersion := baselineSnapshot.Version
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Date":"not-a-date","Valute":{}}`))
	}))
	defer server.Close()
	previousEndpoint := modelCostFXEndpoint
	modelCostFXEndpoint = server.URL
	t.Cleanup(func() { modelCostFXEndpoint = previousEndpoint })

	_, err := Refresh().Execute(context.Background())
	require.Error(t, err)
	snapshot, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, baselineVersion, snapshot.Version)
	assert.Equal(t, baselineSnapshot.Rates["USD"], snapshot.Rates["USD"])
}

func TestModelCostFXRefreshRejectsInvalidProviderResponses(t *testing.T) {
	withModelCostFXServiceDialect(t)
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"non200", http.StatusBadGateway, `{}`, ""},
		{"missing usd", http.StatusOK, `{"Date":"2026-09-08T11:30:00+03:00","Valute":{}}`, ""},
		{"zero nominal", http.StatusOK, `{"Date":"2026-09-08T11:30:00+03:00","Valute":{"USD":{"CharCode":"USD","Value":90,"Nominal":0}}}`, ""},
		{"zero value", http.StatusOK, `{"Date":"2026-09-08T11:30:00+03:00","Valute":{"USD":{"CharCode":"USD","Value":0,"Nominal":1}}}`, ""},
		{"invalid json", http.StatusOK, `{"Date":`, ""},
	}
	for _, value := range []struct{ name, body, kind string }{
		{"root number", "123", "number"}, {"root array", "[]", "array"}, {"root string", `"x"`, "string"}, {"root bool", "true", "bool"},
	} {
		cases = append(cases, struct {
			name   string
			status int
			body   string
			want   string
		}{value.name, http.StatusOK, value.body, "decode model cost FX provider response: json: cannot unmarshal " + value.kind + " into Go value of type service.modelCostFXProviderResponse"})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseline := seedModelCostFXServiceTest(t)
			baselineVersion := baseline.Version
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			previousEndpoint := modelCostFXEndpoint
			modelCostFXEndpoint = server.URL
			t.Cleanup(func() { modelCostFXEndpoint = previousEndpoint })
			_, err := Refresh().Execute(context.Background())
			require.Error(t, err)
			if tc.want != "" {
				assert.EqualError(t, err, tc.want)
			}
			current, currentErr := fxstate.CurrentModelCostFX(modelCostFXSource)
			require.NoError(t, currentErr)
			assert.Equal(t, baselineVersion, current.Version)
		})
	}
}

func TestModelCostFXRefreshCancellationFailsBeforeFetch(t *testing.T) {
	withModelCostFXServiceDialect(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ModelCostFXRow{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Refresh().Execute(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestModelCostFXRefreshRejectsConcurrentWinnerWithoutRetry(t *testing.T) {
	withModelCostFXServiceDialect(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ModelCostFXRow{}))
	var existing model.ModelCostFXRow
	if err := model.DB.Where("source = ?", modelCostFXSource).First(&existing).Error; err != nil {
		require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{Source: modelCostFXSource, EffectiveAt: 100, FetchedAt: 200, Rates: map[string]float64{"USD": 90}}, 0))
	}
	require.NoError(t, fxstorage.LoadModelCostFX(context.Background(), modelCostFXSource))
	baseline, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	started := make(chan struct{})
	release := make(chan struct{})
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"Date":"2026-09-08T11:30:00+03:00","Valute":{"USD":{"CharCode":"USD","Value":91,"Nominal":1}}}`))
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	previousEndpoint := modelCostFXEndpoint
	modelCostFXEndpoint = server.URL
	t.Cleanup(func() { modelCostFXEndpoint = previousEndpoint })

	resultCh := make(chan error, 1)
	go func() {
		_, err := Refresh().Execute(context.Background())
		resultCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start HTTP")
	}
	require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSnapshot{Source: modelCostFXSource, EffectiveAt: baseline.EffectiveAt + 100, FetchedAt: baseline.FetchedAt + 100, Rates: map[string]float64{"USD": 91}}, baseline.Version))
	close(release)
	select {
	case result := <-resultCh:
		require.ErrorIs(t, result, pricingdomainfx.ErrModelCostFXVersionConflict)
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish")
	}
	assert.Equal(t, 1, requests)
	row := model.ModelCostFXRow{}
	require.NoError(t, model.DB.Where("source = ?", modelCostFXSource).First(&row).Error)
	assert.Equal(t, baseline.Version+1, row.Version)
	snapshot, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, baseline.Version+1, snapshot.Version)
	assert.Equal(t, 91.0, snapshot.Rates["USD"])
}

func TestModelCostFXReplicaReloadKeepsAndRefreshesLastGood(t *testing.T) {
	withModelCostFXServiceDialect(t)
	seed := seedModelCostFXServiceTest(t)
	var before model.ModelCostFXRow
	require.NoError(t, model.DB.Where("source = ?", modelCostFXSource).First(&before).Error)
	loadModelCostFXFromDatabase()
	var unchanged model.ModelCostFXRow
	require.NoError(t, model.DB.Where("source = ?", modelCostFXSource).First(&unchanged).Error)
	assert.Equal(t, before.Version, unchanged.Version)
	assert.Equal(t, before.FetchedAt, unchanged.FetchedAt)

	nextRates := `{"USD":91}`
	require.NoError(t, model.DB.Model(&model.ModelCostFXRow{}).Where("source = ?", modelCostFXSource).Updates(map[string]any{
		"effective_at": seed.EffectiveAt + 1, "fetched_at": seed.FetchedAt + 1, "version": seed.Version + 1, "rates_json": nextRates,
	}).Error)
	loadModelCostFXFromDatabase()
	current, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, seed.Version+1, current.Version)
	assert.Equal(t, 91.0, current.Rates["USD"])

	var valid model.ModelCostFXRow
	require.NoError(t, model.DB.Where("source = ?", modelCostFXSource).First(&valid).Error)
	require.NoError(t, model.DB.Model(&model.ModelCostFXRow{}).Where("source = ?", modelCostFXSource).Update("rates_json", "{bad").Error)
	loadModelCostFXFromDatabase()
	retained, err := fxstate.CurrentModelCostFX(modelCostFXSource)
	require.NoError(t, err)
	assert.Equal(t, current.Version, retained.Version)
	assert.Equal(t, 91.0, retained.Rates["USD"])
	require.NoError(t, model.DB.Model(&model.ModelCostFXRow{}).Where("source = ?", modelCostFXSource).Updates(map[string]any{"rates_json": valid.RatesJSON}).Error)
}
