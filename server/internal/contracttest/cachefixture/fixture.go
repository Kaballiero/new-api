// Package cachefixture preserves the private SQLite and Redis fixtures of the shared quota/cache tests.
package cachefixture

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

// Open preserves the original native fixture's single-connection, in-memory SQLite database.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousGroupColumn, previousKeyColumn := model.GroupColumn, model.KeyColumn
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousConsume := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.GroupColumn, model.KeyColumn = "`group`", "`key`"
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	t.Cleanup(func() {
		require.NoError(t, pool.Close())
		model.DB, model.LOG_DB = previousDB, previousLogDB
		model.GroupColumn, model.KeyColumn = previousGroupColumn, previousKeyColumn
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousConsume
	})
	return db
}

// NewRedis retains the existing short-cache-window fixture and restores process settings.
func NewRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB, oldSyncFrequency := common.RedisEnabled, common.RDB, common.SyncFrequency
	common.RedisEnabled, common.SyncFrequency = true, 2
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB, common.SyncFrequency = oldRedisEnabled, oldRDB, oldSyncFrequency
	})
	return server
}
