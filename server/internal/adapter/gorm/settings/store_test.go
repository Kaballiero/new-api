package settings

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/settings/record"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"

	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Each configured engine gets private data; PostgreSQL also scopes every pooled connection.
func TestOptionsPersistence(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialect gorm.Dialector
			switch engine {
			case "sqlite":
				dialect = sqlite.Open(filepath.Join(t.TempDir(), "options.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				config, err := sqlmysql.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "tcp", config.Net)
				host, _, err := net.SplitHostPort(config.Addr)
				require.NoError(t, err)
				require.True(t, net.ParseIP(host).IsLoopback(), "database tests only permit loopback instances")
				config.DBName = ""
				admin, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
				require.NoError(t, err)
				config.DBName = fmt.Sprintf("options_%d", time.Now().UnixNano())
				require.NoError(t, admin.Exec("CREATE DATABASE "+config.DBName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error)
				t.Cleanup(func() {
					require.NoError(t, admin.Exec("DROP DATABASE "+config.DBName).Error)
					pool, err := admin.DB()
					require.NoError(t, err)
					require.NoError(t, pool.Close())
				})
				dialect = mysql.Open(config.FormatDSN())
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialect = postgres.Open(pgfixture.ScopedDSN(t, dsn))
			}
			db, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pool.Close()) })
			if engine == "postgres" {
				pgfixture.ProbePool(t, db)
			}
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Log("database version:", version)
			require.NoError(t, db.AutoMigrate(&record.Option{}))
			store := NewStore(func() *gorm.DB { return db })
			missing, err := store.ReadOption(context.Background(), "Footer")
			require.NoError(t, err)
			assert.Empty(t, missing, "a missing row reads as an empty value")
			store.SaveLegacy("Footer", "before Ω")
			stored, err := store.ReadOption(context.Background(), "Footer")
			require.NoError(t, err)
			assert.Equal(t, "before Ω", stored)
			require.NoError(t, db.AutoMigrate(&record.Option{}))
			require.NoError(t, db.AutoMigrate(&record.Option{}))
			require.NoError(t, store.SaveBulk(context.Background(), []string{"About", "Footer"}, map[string]string{"About": "", "Footer": "after Ω"}))
			rows, err := store.All()
			require.NoError(t, err)
			values := map[string]string{}
			for _, row := range rows {
				values[row.Key] = row.Value
			}
			assert.Equal(t, map[string]string{"About": "", "Footer": "after Ω"}, values)
			stored, err = store.ReadOption(context.Background(), "Footer")
			require.NoError(t, err)
			assert.Equal(t, "after Ω", stored)
			duplicate := record.Option{Key: "Footer", Value: "duplicate"}
			require.Error(t, db.Create(&duplicate).Error, "existing primary-key uniqueness survives repeated migration")

			fault := errors.New("injected option update failure")
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:option_write", func(tx *gorm.DB) {
				if tx.Statement.Table == "options" {
					tx.AddError(fault)
				}
			}))
			store.SaveLegacy("Footer", "ignored error")
			err = store.SaveBulk(context.Background(), []string{"A-new", "Footer"}, map[string]string{"A-new": "rollback", "Footer": "blocked"})
			require.ErrorIs(t, err, fault)
			require.NoError(t, db.Callback().Update().Remove("test:option_write"))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			require.ErrorIs(t, store.SaveBulk(ctx, []string{"Footer"}, map[string]string{"Footer": "cancelled"}), context.Canceled)
			_, err = store.ReadOption(ctx, "Footer")
			require.ErrorIs(t, err, context.Canceled)

			reopened, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			reopenedPool, err := reopened.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reopenedPool.Close()) })
			if engine == "postgres" {
				pgfixture.ProbePool(t, reopened)
			}
			rows, err = NewStore(func() *gorm.DB { return reopened }).All()
			require.NoError(t, err)
			values = map[string]string{}
			for _, row := range rows {
				values[row.Key] = row.Value
			}
			assert.Equal(t, map[string]string{"About": "", "Footer": "after Ω"}, values, "legacy failure and checked rollback leave the existing rows intact")
		})
	}
}
