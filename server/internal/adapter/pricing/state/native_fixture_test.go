package pricingstate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/record"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	ensure_catalog_source "github.com/QuantumNous/new-api/internal/pricing/ensure_catalog_source"
	get_pricing_config "github.com/QuantumNous/new-api/internal/pricing/get_pricing_config"
	publish_catalog_selection "github.com/QuantumNous/new-api/internal/pricing/publish_catalog_selection"
	update_pricing_config "github.com/QuantumNous/new-api/internal/pricing/update_pricing_config"
	legacy "github.com/QuantumNous/new-api/model"
	config "github.com/QuantumNous/new-api/setting/config"
	ratio_setting "github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/glebarez/sqlite"
	assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	schema "gorm.io/gorm/schema"
)

func pricingCatalogOptionFixture(t *testing.T) (*gorm.DB, *legacy.Channel, pricingdomain.PricingCatalogEntry) {
	t.Helper()
	db := useGroupRatioOptionDB(t)
	require.NoError(t, db.AutoMigrate(&legacy.Channel{}))
	require.NoError(t, db.Where("1 = 1").Delete(&legacy.Channel{}).Error)
	previous := pricingstate.TestPricingCatalogState.Load()
	pricingstate.TestPricingCatalogState.Store(nil)
	t.Cleanup(func() { pricingstate.TestPricingCatalogState.Store(previous); pricingstate.InvalidatePricingCache() })
	channel := &legacy.Channel{
		Id:     970,
		Type:   constant.ChannelTypeOpenRouter,
		Status: common.ChannelStatusEnabled,
		Key:    "private-fixture",
		Models: "upstream/model",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	amount := .03
	entry := pricingdomain.PricingCatalogEntry{
		ChannelID:     channel.Id,
		UpstreamModel: "upstream/model",
		Endpoint:      constant.EndpointTypeImageGeneration,
		Source:        "https://openrouter.ai/api/v1/images/models/upstream/model/endpoints",
		FetchedAt:     "2026-10-13T13:41:19Z",
		Fingerprint: pricingstate.PricingCatalogSourceFingerprint(
			channel,
			"upstream/model",
			constant.EndpointTypeImageGeneration,
		),
		Offers: []pricingdomain.CatalogOffer{{
			ID:            "native",
			Operation:     "image_generation",
			Mode:          "sync",
			MainComponent: "image_output",
			Prices: []pricingdomain.CatalogPrice{{
				Component: "image_output",
				Unit:      "megapixel",
				AmountUSD: &amount,
			}},
		}},
	}
	return db, channel, entry
}

func requireOptionValue(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var row legacy.Option
	require.NoError(t, db.Where("key = ?", key).First(&row).Error)
	return row.Value
}
func openPricingBatchDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	if dialect == "postgres" {
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		db := pgfixture.Open(t, dsn)
		previous := common.MainDatabaseType()
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		require.NoError(t, db.AutoMigrate(&legacy.ModelCostFXRow{}))
		t.Cleanup(func() {
			require.NoError(t, db.Migrator().DropTable(&legacy.ModelCostFXRow{}))
			common.SetMainDatabaseType(previous)
		})
		return db
	}
	var driver gorm.Dialector = sqlite.Open(filepath.Join(t.TempDir(), "model-cost-fx.db"))
	if dialect == "mysql" {
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	previous := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseType(dialect))
	t.Cleanup(func() { common.SetMainDatabaseType(previous) })
	require.NoError(t, db.AutoMigrate(&legacy.ModelCostFXRow{}))
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&legacy.ModelCostFXRow{})
		raw, err := db.DB()
		if err == nil {
			_ = raw.Close()
		}
	})
	return db
}
func TestBatchModelPricing(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openPricingBatchDB(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_pricing_"}
			previousDB, previousKeyCol := legacy.DB, legacy.KeyColumn
			previousOptions := common.OptionMap
			previousConfigs := config.GlobalConfig.ExportAllConfigs()
			previousRatios := map[string]string{
				"ModelPrice":           ratio_setting.ModelPrice2JSONString(),
				"ModelRatio":           ratio_setting.ModelRatio2JSONString(),
				"CompletionRatio":      ratio_setting.CompletionRatio2JSONString(),
				"CacheRatio":           ratio_setting.CacheRatio2JSONString(),
				"CreateCacheRatio":     ratio_setting.CreateCacheRatio2JSONString(),
				"ImageRatio":           ratio_setting.ImageRatio2JSONString(),
				"AudioRatio":           ratio_setting.AudioRatio2JSONString(),
				"AudioCompletionRatio": ratio_setting.AudioCompletionRatio2JSONString(),
			}
			legacy.DB = db
			legacy.KeyColumn = "`key`"
			if dialect == "postgres" {
				legacy.KeyColumn = `"key"`
			}
			common.OptionMap = make(map[string]string)
			t.Cleanup(func() {
				for key, value := range previousConfigs {
					require.NoError(t, publication.PublishOption(key, value))
				}
				for key, value := range previousRatios {
					require.NoError(t, publication.PublishOption(key, value))
				}
				common.OptionMap = previousOptions
				legacy.DB, legacy.KeyColumn = previousDB, previousKeyCol
				_ = db.Migrator().DropTable(&legacy.Option{})
			})
			require.NoError(t, db.AutoMigrate(&legacy.Option{}, &legacy.Channel{}, &legacy.Model{}, &legacy.Vendor{}))
			t.Cleanup(func() { _ = db.Migrator().DropTable(&legacy.Vendor{}, &legacy.Model{}, &legacy.Channel{}) })
			for _, table := range []struct {
				name  string
				value any
			}{{"channels", &legacy.Channel{}}, {"abilities", &channelrecord.Ability{}}} {
				if !db.Migrator().HasTable(table.name) {
					require.NoError(t, db.Table(table.name).AutoMigrate(table.value))
					t.Cleanup(func() { _ = db.Migrator().DropTable(table.name) })
				}
			}
			snapshot, err := getModelPricingSnapshot([]string{"batch-priced-model"})
			require.NoError(t, err)
			require.Len(t, snapshot.Entries, 1)
			values := pricingdomain.PricingValues{"ModelRatio": float64(1), "billing_setting.batch_billing_expr": "p * 0.5 + c * 1"}
			change := pricingdomain.ModelPricingChange{ModelName: "batch-priced-model", ExpectedVersion: snapshot.Entries[0].Version, Pricing: values}
			require.NoError(t, updateModelPricing([]pricingdomain.ModelPricingChange{change}))
			saved, err := getModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, values, saved.Entries[0].Configured)
			assert.ErrorIs(t, updateModelPricing([]pricingdomain.ModelPricingChange{change}), pricingdomain.ErrModelPricingConflict)
			for _, invalid := range []string{"p *", `p * header("x-rate")`} {
				assert.Error(
					t,
					updateModelPricing([]pricingdomain.ModelPricingChange{{
						ModelName:       change.ModelName,
						ExpectedVersion: saved.Entries[0].Version,
						Pricing:         pricingdomain.PricingValues{"billing_setting.batch_billing_expr": invalid},
					}}),
				)
			}
			unchanged, err := getModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, saved.Entries[0].Version, unchanged.Entries[0].Version)
			require.NoError(
				t,
				updateModelPricing([]pricingdomain.ModelPricingChange{{
					ModelName:       change.ModelName,
					ExpectedVersion: saved.Entries[0].Version,
					Pricing:         pricingdomain.PricingValues{"ModelRatio": float64(1)},
				}}),
			)
			cleared, err := getModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, pricingdomain.PricingValues{"ModelRatio": float64(1)}, cleared.Entries[0].Configured)
		})
	}
}

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	legacy.DB, legacy.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	legacy.GroupColumn, legacy.KeyColumn = "`group`", "`key`"
	raw, err := db.DB()
	if err != nil {
		panic(err)
	}
	raw.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&legacy.Channel{}, &channelrecord.Ability{}, &legacy.Model{}, &legacy.Vendor{}, &legacy.Option{}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func getModelPricingSnapshot(names []string) (*pricingdomain.ModelPricingSnapshot, error) {
	return pricingruntime.ModelPricingReader().Execute(get_pricing_config.Input{Models: names})
}
func updateModelPricing(changes []pricingdomain.ModelPricingChange) error {
	return update_pricing_config.NewUsecase(
		pricingadapter.Capabilities(),
		pricingruntime.NewPricingOptionsPersistence(),
		pricingruntime.NewPricingConfiguration(),
		common.SysError,
		pricingruntime.RefreshPricing,
		ratio_setting.InvalidateExposedDataCache,
		func([]string) {},
	).Execute(changes)
}
func publishPricingCatalogEntry(entry pricingdomain.PricingCatalogEntry) error {
	return publishPricingCatalogEntryContext(context.Background(), entry)
}
func publishPricingCatalogEntryContext(ctx context.Context, entry pricingdomain.PricingCatalogEntry) error {
	count, err := pricingruntime.NativePublication().Execute(ctx, []pricingdomain.PricingCatalogEntry{entry}, false)
	if err == nil && count == 0 {
		return fmt.Errorf("pricing catalog source changed before publication")
	}
	return err
}

func ensureCatalogSource(ctx context.Context, source pricingrecord.CatalogSource) (pricingrecord.CatalogSource, error) {
	value, err := ensure_catalog_source.NewUsecase(pricingstate.NewImmutableCatalogPersistence()).Execute(ctx, pricingdomain.CatalogSource(source))
	return pricingrecord.CatalogSource(value), err
}
func publishCatalogSelection(
	ctx context.Context,
	expected pricingrecord.CatalogSource,
	selection pricingdomain.CatalogSelection,
	remote, fetched string,
	documents []pricingrecord.CatalogDocument,
) (pricingdomain.CatalogPublication, error) {
	var values []pricingdomain.CatalogDocument
	for _, value := range documents {
		values = append(values, pricingdomain.CatalogDocument(value))
	}
	return publish_catalog_selection.NewUsecase(
		pricingadapter.Capabilities(),
		pricingstate.NewImmutableCatalogPersistence(),
		time.Now,
	).Execute(
		ctx,
		pricingdomain.CatalogSource(expected),
		selection,
		remote,
		fetched,
		values,
	)
}
