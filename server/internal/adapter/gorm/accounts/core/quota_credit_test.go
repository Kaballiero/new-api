package core

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/credit_getapi_quota"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
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
	require.NoError(t, db.AutoMigrate(&record.GetAPIQuotaCredit{}))
	require.NoError(t, db.Create(&record.GetAPIQuotaCredit{OperationKey: []byte("duplicate"), UserID: 1, Quota: 1}).Error)
	sqliteUnique := db.Create(&record.GetAPIQuotaCredit{OperationKey: []byte("duplicate"), UserID: 1, Quota: 1}).Error
	require.Error(t, sqliteUnique)
	sqliteNotNull := db.Model(&record.GetAPIQuotaCredit{}).Where("operation_key = ?", []byte("duplicate")).Update("operation_key", nil).Error
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

// The HTTP suite covers auth and Redis; this fixture exercises the transaction
// independently of listeners, including receipt replay and wallet rollback.
func TestQuotaCreditTransactionPreservesReceiptAndCommittedCacheDelta(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&record.User{}, &record.GetAPIQuotaCredit{}))
	user := record.User{Username: "credit-transaction", AffCode: "credit-transaction", Quota: 100}
	require.NoError(t, db.Create(&user).Error)
	store := NewStore(Environment{DB: func() *gorm.DB { return db }})
	var cacheDelta int64
	scenario := credit_getapi_quota.NewUsecase(store, common.MaxWalletQuota, common.WalletQuotaFromDecimalStrict, func() int64 { return 123 },
		func(id int, delta int64) error { assert.Equal(t, user.Id, id); cacheDelta += delta; return nil }, func(_ int, err error) { require.NoError(t, err) })
	in := credit_getapi_quota.Input{UserID: user.Id, OperationKey: "payment:1", Quota: 50}
	first, err := scenario.Execute(in)
	require.NoError(t, err)
	assert.Equal(t, &credit_getapi_quota.GetAPIQuotaCreditResult{Applied: true, Quota: 150}, first)
	require.NoError(t, db.Model(&user).Update("quota", 145).Error)
	replay, err := scenario.Execute(in)
	require.NoError(t, err)
	assert.Equal(t, &credit_getapi_quota.GetAPIQuotaCreditResult{AlreadyApplied: true, Quota: 145}, replay)
	assert.EqualValues(t, 50, cacheDelta)
	conflicting := in
	conflicting.Quota++
	_, err = scenario.Execute(conflicting)
	assert.ErrorIs(t, err, domain.ErrGetAPIQuotaCreditConflict)
	require.NoError(t, db.Model(&user).Update("quota", common.MaxWalletQuota).Error)
	_, err = scenario.Execute(credit_getapi_quota.Input{UserID: user.Id, OperationKey: "overflow", Quota: 1})
	assert.ErrorIs(t, err, domain.ErrQuotaLimitExceeded)
	var receipts int64
	require.NoError(t, db.Model(&record.GetAPIQuotaCredit{}).Where("operation_key = ?", []byte("overflow")).Count(&receipts).Error)
	assert.Zero(t, receipts)
	require.NoError(t, db.Delete(&user).Error)
	replay, err = scenario.Execute(in)
	require.NoError(t, err)
	assert.True(t, replay.AlreadyApplied)
	assert.Equal(t, common.MaxWalletQuota, replay.Quota)
	assert.EqualValues(t, 50, cacheDelta)
}
