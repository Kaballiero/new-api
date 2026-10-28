// Package fxfixture owns the shared native FX/Batch database setup.
package fxfixture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/record"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Open(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	switch dialect {
	case "sqlite":
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-cost-fx.db")), &gorm.Config{})
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		db = pgfixture.Open(t, dsn)
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Logf("database version: %s", version)
	default:
		t.Fatalf("unsupported test dialect %q", dialect)
	}
	require.NoError(t, err)
	previousType := common.MainDatabaseType()
	switch dialect {
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	default:
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	}
	t.Cleanup(func() { common.SetMainDatabaseType(previousType) })
	require.NoError(t, db.AutoMigrate(&record.ModelCostFXRow{}))
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&record.ModelCostFXRow{})
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
