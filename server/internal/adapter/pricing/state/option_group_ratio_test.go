package pricingstate_test

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// MySQL requires a private database; PostgreSQL always gets a private startup search_path.
func useGroupRatioOptionDB(t *testing.T) *gorm.DB {
	t.Helper()
	var dialect gorm.Dialector = sqlite.Open(":memory:")
	if dsn := os.Getenv("GROUP_RATIO_TEST_MYSQL_DSN"); dsn != "" {
		dialect = mysql.Open(dsn)
	} else if dsn := os.Getenv("GROUP_RATIO_TEST_POSTGRES_DSN"); dsn != "" {
		dialect = postgres.Open(pgfixture.ScopedDSN(t, dsn))
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	require.NoError(t, err)
	if db.Dialector.Name() == "postgres" {
		pgfixture.ProbePool(t, db)
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Log("database version:", version)
	}
	previousDialect := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseType(db.Dialector.Name()))
	t.Cleanup(func() { common.SetMainDatabaseType(previousDialect) })
	previousGroupCol, previousKeyCol := legacy.GroupColumn, legacy.KeyColumn
	legacy.GroupColumn, legacy.KeyColumn = "`group`", "`key`"
	if db.Dialector.Name() == "postgres" {
		legacy.GroupColumn, legacy.KeyColumn = `"group"`, `"key"`
	}
	t.Cleanup(func() { legacy.GroupColumn, legacy.KeyColumn = previousGroupCol, previousKeyCol })

	require.NoError(t, db.AutoMigrate(&legacy.Option{}))
	require.NoError(t, db.Where("1 = 1").Delete(&legacy.Option{}).Error)
	previousDB, previousOptions := legacy.DB, common.OptionMap
	previousRatio := ratio_setting.GroupRatio2JSONString()
	previousSpecial := ratio_setting.GroupGroupRatio2JSONString()
	legacy.DB, common.OptionMap = db, map[string]string{}
	t.Cleanup(func() {
		legacy.DB, common.OptionMap = previousDB, previousOptions
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatio))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousSpecial))
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}
