package batchfixture

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var batchDatabaseDSNs sync.Map

func PostgresDSN(t *testing.T, dsn string) string { return pgfixture.ScopedDSN(t, dsn) }
func ProbePool(t *testing.T, db *gorm.DB)         { pgfixture.ProbePool(t, db) }

// OpenDialect preserves env-driven engine selection and initializes model's
// existing technical column/cache owner through the production startup path.
func OpenDialect(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	dsn := ""
	switch dialect {
	case "postgres":
		dsn = os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		dsn = PostgresDSN(t, dsn)
		var err error
		dsn, err = postgresURL(dsn)
		require.NoError(t, err)
	case "mysql":
		dsn = os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
	case "sqlite":
	default:
		t.Fatalf("unsupported Batch test dialect %q", dialect)
	}
	oldDB, oldType, oldLogType, oldMaster, oldSQLite := model.DB, common.MainDatabaseType(),
		common.LogDatabaseType(), common.IsMasterNode, common.SQLitePath
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode = false
	common.SQLitePath = filepath.Join(t.TempDir(), "batch-main.db")
	err := model.InitDB()
	db := model.DB
	model.DB, common.IsMasterNode, common.SQLitePath = oldDB, oldMaster, oldSQLite
	require.NoError(t, err)
	// InitDB parses model types during startup. A fresh GORM schema cache lets
	// each native fixture install its naming strategy before its first query.
	initialized := db
	initialPool, poolErr := initialized.DB()
	require.NoError(t, poolErr)
	t.Cleanup(func() { require.NoError(t, initialPool.Close()) })
	db, err = gorm.Open(initialized.Dialector, &gorm.Config{})
	require.NoError(t, err)
	batchDatabaseDSNs.Store(db, dsn)
	t.Cleanup(func() {
		batchDatabaseDSNs.Delete(db)
		pool, e := db.DB()
		require.NoError(t, e)
		require.NoError(t, pool.Close())
		common.SetDatabaseTypes(oldType, oldLogType)
	})
	if dialect == "postgres" {
		ProbePool(t, db)
	}
	var version string
	query := "SELECT VERSION()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("real %s %s", dialect, version)
	return db
}
func ReopenPostgres(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	value, ok := batchDatabaseDSNs.Load(db)
	require.True(t, ok, "missing captured scoped PostgreSQL DSN")
	dsn, ok := value.(string)
	require.True(t, ok)
	reopened, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NamingStrategy: db.Config.NamingStrategy})
	require.NoError(t, err)
	t.Cleanup(func() { p, e := reopened.DB(); require.NoError(t, e); require.NoError(t, p.Close()) })
	ProbePool(t, reopened)
	return reopened
}

// postgresURL retains every keyword parameter while selecting the PostgreSQL
// startup path, which recognizes URL DSNs. Values keep libpq quoting/escaping.
func postgresURL(dsn string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return dsn, nil
	}
	query := url.Values{}
	for input := strings.Trim(dsn, " \t\n\r\v\f"); input != ""; input = strings.Trim(input, " \t\n\r\v\f") {
		index := strings.IndexByte(input, '=')
		if index < 1 {
			return "", errors.New("invalid PostgreSQL test DSN key")
		}
		key := strings.Trim(input[:index], " \t\n\r\v\f")
		input = strings.TrimLeft(input[index+1:], " \t\n\r\v\f")
		var value strings.Builder
		quoted := strings.HasPrefix(input, "'")
		if quoted {
			input = input[1:]
		}
		closed := !quoted
		for len(input) > 0 {
			char := input[0]
			if char == '\\' {
				if len(input) < 2 {
					return "", errors.New("invalid PostgreSQL test DSN escape")
				}
				value.WriteByte(char)
				value.WriteByte(input[1])
				input = input[2:]
				continue
			}
			if quoted && char == '\'' {
				input = input[1:]
				closed = true
				break
			}
			if !quoted && strings.ContainsRune(" \t\n\r\v\f", rune(char)) {
				break
			}
			value.WriteByte(char)
			input = input[1:]
		}
		if !closed {
			return "", errors.New("invalid PostgreSQL test DSN quote")
		}
		decoded := strings.ReplaceAll(strings.ReplaceAll(value.String(), `\\`, `\`), `\'`, `'`)
		query.Set(key, decoded)
	}
	return "postgresql:///?" + query.Encode(), nil
}
