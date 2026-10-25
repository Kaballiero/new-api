package batchfixture

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresURLPreservesStartupSettings(t *testing.T) {
	for _, dsn := range []string{
		"host=localhost port=5432 user=fixture password=unused dbname=batch sslmode=disable search_path=private_batch",
		"host='/tmp/fixture socket' user=fixture password='unused \\'quote' dbname=batch sslmode=disable application_name='batch tests'",
		"host=localhost user=fixture password=unused\\ space dbname=batch sslmode=disable search_path=private_batch",
		"postgres://fixture:unused@localhost/batch?sslmode=disable&search_path=private_batch",
	} {
		converted, err := postgresURL(dsn)
		require.NoError(t, err)
		before, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		after, err := pgx.ParseConfig(converted)
		require.NoError(t, err)
		assert.Equal(t, before.Host, after.Host)
		assert.Equal(t, before.Port, after.Port)
		assert.Equal(t, before.User, after.User)
		assert.Equal(t, before.Password, after.Password)
		assert.Equal(t, before.Database, after.Database)
		assert.Equal(t, before.RuntimeParams, after.RuntimeParams)
		assert.Equal(t, before.TLSConfig, after.TLSConfig)
	}
}

func TestPostgresURLRejectsMalformedKeywordValues(t *testing.T) {
	for _, dsn := range []string{"host", "host='unterminated", "password=unfinished\\"} {
		_, err := postgresURL(dsn)
		require.Error(t, err)
	}
}
