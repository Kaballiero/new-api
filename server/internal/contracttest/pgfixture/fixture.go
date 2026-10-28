package pgfixture

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ScopedDSN allocates one private schema; cleanup captures both the admin pool and schema.
func ScopedDSN(t *testing.T, dsn string) string {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "wave5b_" + strings.ToLower(common.GetRandomString(20))
	created := false
	t.Cleanup(func() {
		pool, e := admin.DB()
		require.NoError(t, e)
		defer pool.Close()
		if !created {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, admin.WithContext(ctx).Exec(`DROP SCHEMA "`+schema+`" CASCADE`).Error)
	})
	result := admin.Exec(`CREATE SCHEMA "` + schema + `"`)
	created = result.Error == nil
	require.NoError(t, result.Error)
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		require.NoError(t, e)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " search_path=" + schema
}

// ProbePool requires two simultaneous connections to observe the same schema.
func ProbePool(t *testing.T, db *gorm.DB) {
	t.Helper()
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, err := pool.Conn(ctx)
	require.NoError(t, err)
	defer a.Close()
	b, err := pool.Conn(ctx)
	require.NoError(t, err)
	defer b.Close()
	var left, right string
	require.NoError(t, a.QueryRowContext(ctx, "SELECT current_schema()").Scan(&left))
	require.NoError(t, b.QueryRowContext(ctx, "SELECT current_schema()").Scan(&right))
	require.Equal(t, left, right)
	require.True(t, strings.HasPrefix(left, "wave5b_"), fmt.Sprintf("unexpected schema %q", left))
}

func Open(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(ScopedDSN(t, dsn)), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { pool, e := db.DB(); require.NoError(t, e); require.NoError(t, pool.Close()) })
	ProbePool(t, db)
	return db
}
