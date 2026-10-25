package model_test

import (
	core "github.com/QuantumNous/new-api/internal/app/batch"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchfixture"
	. "github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestBatchSeparateLogDatabase(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres", "clickhouse"} {
		t.Run(dialect, func(t *testing.T) {
			primary, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "primary.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, primary.AutoMigrate(&BatchLogEvent{}))
			var logDriver gorm.Dialector
			kind := common.DatabaseTypeSQLite
			switch dialect {
			case "mysql":
				dsn := os.Getenv("TEST_LOG_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_MYSQL_DSN is not configured")
				}
				logDriver = mysql.Open(dsn + "&clientFoundRows=true")
				kind = common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_LOG_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_POSTGRES_DSN is not configured")
				}
				logDriver = postgres.Open(fixture.PostgresDSN(t, dsn))
				kind = common.DatabaseTypePostgreSQL
			case "clickhouse":
				dsn := os.Getenv("TEST_LOG_CLICKHOUSE_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_CLICKHOUSE_DSN is not configured")
				}
				logDriver = clickhouse.Open(dsn)
				kind = common.DatabaseTypeClickHouse
			default:
				logDriver = sqlite.Open(filepath.Join(t.TempDir(), "logs.db"))
			}
			logs, err := gorm.Open(logDriver, &gorm.Config{})
			require.NoError(t, err)
			if dialect == "postgres" {
				fixture.ProbePool(t, logs)
			}
			oldDB, oldLogDB, oldKind := DB, LOG_DB, common.LogDatabaseType()
			DB, LOG_DB = primary, logs
			common.SetLogDatabaseType(kind)
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLogDB
				common.SetLogDatabaseType(oldKind)
				for _, db := range []*gorm.DB{primary, logs} {
					sqlDB, e := db.DB()
					if e == nil {
						_ = sqlDB.Close()
					}
				}
			})
			if dialect == "clickhouse" {
				require.NoError(t, logs.Exec(BatchTestClickHouseDDL()).Error)
				require.NoError(t, logs.Exec("TRUNCATE TABLE logs").Error)
			} else {
				logs.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_separate_log_"}
				require.NoError(t, logs.AutoMigrate(&Log{}, &BatchLogReceipt{}))
				t.Cleanup(func() { _ = logs.Migrator().DropTable(&BatchLogReceipt{}, &Log{}) })
				for range 2 {
					require.NoError(t, logs.AutoMigrate(&Log{}, &BatchLogReceipt{}))
				}
			}
			for _, db := range []*gorm.DB{primary, logs} {
				sqlDB, e := db.DB()
				require.NoError(t, e)
				sqlDB.SetMaxOpenConns(1)
			}
			payload, err := common.Marshal(Log{UserId: 1, Type: LogTypeConsume, RequestId: "batch-log-contract", Quota: 10, CreatedAt: common.GetTimestamp(), Other: `{"admin_info":{"quota_saturation":{"kind":"overflow"}}}`})
			require.NoError(t, err)
			event := BatchLogEvent{ItemID: common.GetPointer(int64(1)), State: "pending", Payload: string(payload)}
			require.NoError(t, primary.Create(&event).Error)
			require.NoError(t, core.NewStore().SyncBatchConsumeLogs(10))
			state := "pending"
			if dialect == "clickhouse" {
				state = "writing"
			}
			require.NoError(t, primary.Model(&event).Update("state", state).Error)
			require.NoError(t, core.NewStore().SyncBatchConsumeLogs(10))
			require.NoError(t, core.NewStore().SyncBatchConsumeLogs(10))
			var count int64
			require.NoError(t, logs.Model(&Log{}).Where("request_id = ?", "batch-log-contract").Count(&count).Error)
			assert.EqualValues(t, 1, count, "lost acknowledgement must not duplicate log or expense statistics")
			// A job-scoped receipt uses the same delivery guarantees without a
			// synthetic item identity, including a separately configured log DB.
			payload, err = common.Marshal(Log{UserId: 1, Type: LogTypeConsume, RequestId: "batch-job-log-contract", Quota: 7, CreatedAt: common.GetTimestamp()})
			require.NoError(t, err)
			jobEvent := BatchLogEvent{BatchID: common.GetPointer(int64(1)), State: "pending", Payload: string(payload)}
			require.NoError(t, primary.Create(&jobEvent).Error)
			require.NoError(t, core.NewStore().SyncBatchConsumeLogs(10))
			require.NoError(t, primary.Model(&jobEvent).Update("state", state).Error)
			require.NoError(t, core.NewStore().SyncBatchConsumeLogs(10))
			require.NoError(t, logs.Model(&Log{}).Where("request_id = ?", "batch-job-log-contract").Count(&count).Error)
			assert.EqualValues(t, 1, count, "job receipts must not duplicate the aggregate consume log")
			if dialect == "clickhouse" {
				payload, err = common.Marshal(Log{RequestId: "ambiguous-before-insert", CreatedAt: common.GetTimestamp()})
				require.NoError(t, err)
				require.NoError(t, primary.Create(&BatchLogEvent{ItemID: common.GetPointer(int64(2)), State: "writing", Payload: string(payload)}).Error)
				require.Error(t, core.NewStore().SyncBatchConsumeLogs(10))
				require.NoError(t, logs.Model(&Log{}).Where("request_id = ?", "ambiguous-before-insert").Count(&count).Error)
				assert.Zero(t, count, "unknown insert must not be resent")
			}
		})
	}
}
