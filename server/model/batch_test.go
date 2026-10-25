package model

import (
	"os"

	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"

	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestBatchModelPricing(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openBatchModelDB(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_pricing_"}
			previousDB, previousKeyCol := DB, commonKeyCol
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
			DB = db
			commonKeyCol = "`key`"
			if dialect == "postgres" {
				commonKeyCol = `"key"`
			}
			common.OptionMap = make(map[string]string)
			t.Cleanup(func() {
				for key, value := range previousConfigs {
					require.NoError(t, updateOptionMap(key, value))
				}
				for key, value := range previousRatios {
					require.NoError(t, updateOptionMap(key, value))
				}
				common.OptionMap = previousOptions
				DB, commonKeyCol = previousDB, previousKeyCol
				_ = db.Migrator().DropTable(&Option{})
			})
			require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Model{}, &Vendor{}))
			t.Cleanup(func() { _ = db.Migrator().DropTable(&Vendor{}, &Model{}, &Channel{}) })
			for _, table := range []struct {
				name  string
				value any
			}{{"channels", &Channel{}}, {"abilities", &Ability{}}} {
				if !db.Migrator().HasTable(table.name) {
					require.NoError(t, db.Table(table.name).AutoMigrate(table.value))
					t.Cleanup(func() { _ = db.Migrator().DropTable(table.name) })
				}
			}
			snapshot, err := GetModelPricingSnapshot([]string{"batch-priced-model"})
			require.NoError(t, err)
			require.Len(t, snapshot.Entries, 1)
			values := PricingValues{"ModelRatio": float64(1), "billing_setting.batch_billing_expr": "p * 0.5 + c * 1"}
			change := ModelPricingChange{ModelName: "batch-priced-model", ExpectedVersion: snapshot.Entries[0].Version, Pricing: values}
			require.NoError(t, UpdateModelPricing([]ModelPricingChange{change}))
			saved, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, values, saved.Entries[0].Configured)
			assert.ErrorIs(t, UpdateModelPricing([]ModelPricingChange{change}), ErrModelPricingConflict)
			for _, invalid := range []string{"p *", `p * header("x-rate")`} {
				assert.Error(t, UpdateModelPricing([]ModelPricingChange{{ModelName: change.ModelName, ExpectedVersion: saved.Entries[0].Version, Pricing: PricingValues{"billing_setting.batch_billing_expr": invalid}}}))
			}
			unchanged, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, saved.Entries[0].Version, unchanged.Entries[0].Version)
			require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: change.ModelName, ExpectedVersion: saved.Entries[0].Version, Pricing: PricingValues{"ModelRatio": float64(1)}}}))
			cleared, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, PricingValues{"ModelRatio": float64(1)}, cleared.Entries[0].Configured)
		})
	}
}

func TestBatchAggregateLogMigration(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openBatchModelDB(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_upgrade_"}
			const table = "batch_upgrade_batch_log_events"
			t.Cleanup(func() { _ = db.Migrator().DropTable(table) })
			require.NoError(t, db.Table(table).AutoMigrate(&batchItemLogEventBeforeAggregate{}))
			require.NoError(t, db.Table(table).Create(&batchItemLogEventBeforeAggregate{ItemID: 71, Payload: `{"legacy":true}`, State: "pending"}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&BatchLogEvent{}))
				require.NoError(t, migrateBatchLogScope(db))
			}
			var legacy BatchLogEvent
			require.NoError(t, db.Where("item_id = ?", 71).First(&legacy).Error)
			require.NotNil(t, legacy.ItemID)
			assert.EqualValues(t, 71, *legacy.ItemID)
			assert.Nil(t, legacy.BatchID)
			assert.Equal(t, `{"legacy":true}`, legacy.Payload)
			require.Error(t, db.Create(&BatchLogEvent{ItemID: common.GetPointer(int64(71)), Payload: "duplicate", State: "pending"}).Error)
			for _, id := range []int64{1, 2} {
				require.NoError(t, db.Create(&BatchLogEvent{BatchID: common.GetPointer(id), Payload: "job", State: "pending"}).Error)
			}
			require.Error(t, db.Create(&BatchLogEvent{BatchID: common.GetPointer(int64(1)), Payload: "duplicate", State: "pending"}).Error)
		})
	}
}

func BatchTestClickHouseDDL() string { return clickHouseLogCreateTableSQL(0) }

// Deployed Batch outbox schema before job-scope receipts.
type batchItemLogEventBeforeAggregate struct {
	ID      int64
	ItemID  int64  `gorm:"not null;uniqueIndex"`
	Payload string `gorm:"size:1048576;not null"`
	State   string `gorm:"size:32;not null;index"`
}

func openBatchModelDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	if dialect != "postgres" {
		return openModelCostFXTestDB(t, dialect)
	}
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db := pgfixture.Open(t, dsn)
	previous := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	require.NoError(t, db.AutoMigrate(&ModelCostFXRow{}))
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&ModelCostFXRow{}))
		common.SetMainDatabaseType(previous)
	})
	return db
}
