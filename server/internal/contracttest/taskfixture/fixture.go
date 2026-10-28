// Package taskfixture supplies shared task database fixtures.
package taskfixture

import (
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/model"

	sqliteclock "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Configure disables external persistence and fixes deployment facts for task tests.
func Configure() {
	common.RedisEnabled, common.BatchUpdateEnabled, common.MemoryCacheEnabled = false, false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	common.NodeName, common.Version = "tasks-contract-node", "contract"
	common.QuotaPerUnit = 500000
}

// Open creates private SQLite main/log databases for package-local ticks and accounting.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	InstallDBClock(t)
	Configure()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	open := func(name string) *gorm.DB {
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)+"?_pragma=synchronous(OFF)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		t.Cleanup(func() { pool, err := db.DB(); require.NoError(t, err); require.NoError(t, pool.Close()) })
		return db
	}
	model.DB, model.LOG_DB = open("main.db"), open("logs.db")
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Task{}, &model.Midjourney{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}, &model.TopUp{}, &model.SubscriptionOrder{},
		&model.SystemTask{}, &model.SystemTaskLock{}, &identityrecord.UserSession{}, &model.Log{}, &model.AuditLog{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	return model.DB
}

var clockOnce sync.Once
var clockError error

// InstallDBClock uses the existing SQLite UDF registration seam, never production clock hooks.
func InstallDBClock(t *testing.T) {
	t.Helper()
	clockOnce.Do(func() {
		clockError = sqliteclock.RegisterScalarFunction("strftime", 2, func(_ *sqliteclock.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) != 2 || args[0] != "%s" || args[1] != "now" {
				return nil, fmt.Errorf("unexpected task DB clock expression")
			}
			return strconv.FormatInt(time.Now().Unix(), 10), nil
		})
	})
	require.NoError(t, clockError)
}
