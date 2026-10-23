package audittest

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
	"github.com/QuantumNous/new-api/model"
)

// NewDatabase isolates each fixture: PostgreSQL uses a private schema and startup search_path.
// Other external dialects retain their disposable loopback database fixtures.
func NewDatabase(t *testing.T, kind, dsn string) (*gorm.DB, string) {
	t.Helper()
	if kind == "sqlite" {
		path := t.TempDir() + "/audit.db"
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		return db, path
	}
	switch kind {
	case "mysql", "postgres", "clickhouse":
	default:
		require.Fail(t, "unsupported audit database kind", "kind: %s", kind)
		return nil, ""
	}
	require.NotEmpty(t, dsn)
	if kind == "postgres" {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		name := fmt.Sprintf("newapi_audit_%d", time.Now().UnixNano())
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, admin.Exec(`CREATE SCHEMA "`+name+`"`).Error)
		query := parsed.Query()
		query.Set("search_path", name)
		parsed.RawQuery = query.Encode()
		isolatedDSN := parsed.String()
		db, err := gorm.Open(postgres.Open(isolatedDSN), &gorm.Config{})
		require.NoError(t, err)
		connection, err := db.DB()
		require.NoError(t, err)
		// Keep both connections checked out to verify pool-wide startup parameters.
		for range 2 {
			conn, err := connection.Conn(t.Context())
			require.NoError(t, err)
			defer conn.Close()
			var schema string
			require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema))
			require.Equal(t, name, schema)
		}
		t.Logf("isolated PostgreSQL schema: %s", name)
		t.Cleanup(func() {
			// InitDB/InitLogDB may replace pools; close each pool before dropping the schema.
			seen := map[*gorm.DB]bool{}
			for _, pool := range []*gorm.DB{db, model.DB, model.LOG_DB} {
				if pool == nil || seen[pool] {
					continue
				}
				seen[pool] = true
				if pool != db {
					dialector, ok := pool.Dialector.(*postgres.Dialector)
					if !ok || dialector.Config.DSN != isolatedDSN {
						continue
					}
				}
				connection, err := pool.DB()
				if err == nil {
					_ = connection.Close()
				}
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, admin.WithContext(cleanup).Exec(`DROP SCHEMA "`+name+`" CASCADE`).Error)
			connection, err := admin.DB()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		})
		return db, isolatedDSN
	}
	name := fmt.Sprintf("newapi_audit_%d", time.Now().UnixNano())
	var original, isolated gorm.Dialector
	var newDSN string
	if kind == "mysql" {
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.True(t, net.ParseIP(host).IsLoopback(), "database tests only permit loopback instances")
		original = mysql.Open(dsn)
		config.DBName = name
		newDSN = config.FormatDSN()
		isolated = mysql.Open(newDSN)
	} else {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback(), "database tests only permit loopback instances")
		parsed.Path = "/" + name
		newDSN = parsed.String()
		original = clickhouse.Open(dsn)
		isolated = clickhouse.Open(newDSN)
	}
	admin, err := gorm.Open(original, &gorm.Config{})
	require.NoError(t, err)
	// No IF NOT EXISTS: a collision fails before any test data can be written.
	createSQL := "CREATE DATABASE " + name
	if kind == "mysql" {
		createSQL += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	require.NoError(t, admin.Exec(createSQL).Error)
	sqlDB, err := admin.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	db, err := gorm.Open(isolated, &gorm.Config{})
	require.NoError(t, err)
	t.Logf("isolated database: %s (%s)", name, kind)
	t.Cleanup(func() {
		connection, err := db.DB()
		if err == nil {
			_ = connection.Close()
		}
	})
	return db, newDSN
}

// Seed inserts an explicit persisted event fixture without invoking production recording policy.
func Seed(t *testing.T, entry model.AuditLog) {
	t.Helper()
	if entry.CreatedAt == 0 {
		entry.CreatedAt = common.GetTimestamp()
	}
	if entry.EventId == "" {
		entry.EventId = common.NewRequestId()
	}
	if entry.RequestId == "" {
		entry.RequestId = common.NewRequestId()
	}
	row, err := record.InsertRow(&entry)
	require.NoError(t, err)
	require.NoError(t, model.LOG_DB.Table("audit_logs").Create(row).Error)
}
