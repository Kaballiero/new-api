// Package contracttest supplies a database fixture for billing regression tests.
package contracttest

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// New initializes a private SQLite database through production migrations.
func New(t *testing.T) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousPath, previousMaster, previousRedis := common.SQLitePath, common.IsMasterNode, common.RedisEnabled
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	// A throwaway database needs no durability; without fsync the migrations take
	// milliseconds instead of minutes on slow disks.
	common.SQLitePath = filepath.Join(t.TempDir(), "billing.db") + "?_pragma=busy_timeout(30000)&_pragma=synchronous(OFF)&_txlock=immediate"
	common.IsMasterNode = true
	common.RedisEnabled = false
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.SQLitePath, common.IsMasterNode, common.RedisEnabled = previousPath, previousMaster, previousRedis
	})
}
