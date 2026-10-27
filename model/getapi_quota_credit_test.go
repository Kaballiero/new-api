package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsQuotaCreditUniqueViolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.AutoMigrate(&GetAPIQuotaCredit{}))
	require.NoError(t, db.Create(&GetAPIQuotaCredit{OperationKey: []byte("duplicate"), UserID: 1, Quota: 1}).Error)
	sqliteUnique := db.Create(&GetAPIQuotaCredit{OperationKey: []byte("duplicate"), UserID: 1, Quota: 1}).Error
	require.Error(t, sqliteUnique)
	sqliteNotNull := db.Model(&GetAPIQuotaCredit{}).Where("operation_key = ?", []byte("duplicate")).Update("operation_key", nil).Error
	require.Error(t, sqliteNotNull)

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("unique constraint"), false},
		{"postgres unique", &pgconn.PgError{Code: "23505"}, true},
		{"postgres foreign key", &pgconn.PgError{Code: "23503"}, false},
		{"mysql unique", &mysql.MySQLError{Number: 1062}, true},
		{"mysql deadlock", &mysql.MySQLError{Number: 1213}, false},
		{"sqlite unique", sqliteUnique, true},
		{"sqlite not null", sqliteNotNull, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isQuotaCreditUniqueViolation(tc.err))
			if tc.err != nil {
				assert.Equal(t, tc.want, isQuotaCreditUniqueViolation(fmt.Errorf("insert receipt: %w", tc.err)))
			}
		})
	}
}
