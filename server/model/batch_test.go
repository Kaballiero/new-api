package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/fxfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

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
		return fxfixture.Open(t, dialect)
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
