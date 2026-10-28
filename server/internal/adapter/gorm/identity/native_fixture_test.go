package identity_test

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// nativeFixture retains private SQLite by default; PostgreSQL is an explicit isolated acceptance mode.
func nativeFixture(t *testing.T) {
	t.Helper()
	var db *gorm.DB
	if os.Getenv("WAVE7J_IDENTITY_POSTGRES") == "1" {
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN required for explicit PostgreSQL mode")
		db = pgfixture.Open(t, dsn)
		previousDB, previousLog := model.DB, model.LOG_DB
		previousMain, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
		previousGroup, previousKey := model.GroupColumn, model.KeyColumn
		previousRedis, previousBatch, previousConsume := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
		model.DB, model.LOG_DB = db, db
		common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
		model.GroupColumn, model.KeyColumn = `"group"`, `"key"`
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
		t.Cleanup(func() {
			model.DB, model.LOG_DB = previousDB, previousLog
			common.SetDatabaseTypes(previousMain, previousLogType)
			model.GroupColumn, model.KeyColumn = previousGroup, previousKey
			common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousConsume
		})
		var version string
		require.NoError(t, db.Raw("SHOW server_version").Scan(&version).Error)
		t.Logf("identity-native PostgreSQL server_version=%s", version)
	} else {
		db = cachefixture.Open(t)
	}
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.UserSession{}, &model.AuthFlow{}, &model.ExternalIdentityClaim{}, &model.PasskeyCredential{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.UserOAuthBinding{}, &model.BatchJob{}, &model.Log{}))
}
