package model

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var ErrGetAPIQuotaCreditConflict = errors.New("GETAPI_QUOTA_CREDIT_CONFLICT")

// Binary keys preserve exact identity, including case and trailing spaces, on
// every dialect regardless of the database's default text collation.
type GetAPIQuotaCredit struct {
	ID           int    `gorm:"primaryKey"`
	OperationKey []byte `gorm:"size:800;not null;uniqueIndex"`
	UserID       int    `gorm:"not null"`
	Quota        int    `gorm:"not null"`
	CreatedAt    int64  `gorm:"not null"`
}

func (GetAPIQuotaCredit) TableName() string { return "getapi_quota_credits" }

type GetAPIQuotaCreditResult struct {
	Applied        bool `json:"applied"`
	AlreadyApplied bool `json:"alreadyApplied"`
	Quota          int  `json:"quota"`
}

func CreditGetAPIUserQuota(userID int, operationKey string, quota int) (*GetAPIQuotaCreditResult, error) {
	if userID <= 0 || !utf8.ValidString(operationKey) || utf8.RuneCountInString(operationKey) < 1 || utf8.RuneCountInString(operationKey) > 200 || quota <= 0 {
		return nil, ErrGetAPIInvalidRequest
	}
	if quota > common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}
	var result GetAPIQuotaCreditResult
	apply := func(tx *gorm.DB) error {
		// SQLite has no SELECT FOR UPDATE. Acquire its write lock before reading,
		// avoiding a deferred read-to-write transaction upgrade during concurrent calls.
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Model(&GetAPIQuotaCredit{}).Where("1 = 0").Update("quota", gorm.Expr("quota")).Error; err != nil {
				return err
			}
		}
		// Do not lock the receipt before the user: concurrent inserts take the
		// user lock first. An unlocked lookup also lets replays survive soft deletion.
		var existing GetAPIQuotaCredit
		err := tx.Where("operation_key = ?", []byte(operationKey)).First(&existing).Error
		if err == nil {
			if existing.UserID != userID || existing.Quota != quota {
				return ErrGetAPIQuotaCreditConflict
			}
			var user User
			if err := tx.Unscoped().First(&user, userID).Error; err != nil {
				return err
			}
			result = GetAPIQuotaCreditResult{AlreadyApplied: true, Quota: user.Quota}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		// Resolve a concurrent winner before balance validation: a replay must
		// still succeed if the first credit brought the wallet to its limit.
		credit := GetAPIQuotaCredit{OperationKey: []byte(operationKey), UserID: userID, Quota: quota, CreatedAt: common.GetTimestamp()}
		if err := tx.Create(&credit).Error; err != nil {
			return err
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		after, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(user.Quota)).Add(decimal.NewFromInt(int64(quota))))
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		update := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		result = GetAPIQuotaCreditResult{Applied: true, Quota: after}
		return nil
	}
	err := DB.Transaction(apply)
	// A concurrent request can commit a receipt after the initial lookup,
	// including while we wait for a user that is then soft-deleted. Retry with
	// a fresh snapshot after rollback (also required by PostgreSQL).
	if isQuotaCreditUniqueViolation(err) || errors.Is(err, gorm.ErrRecordNotFound) {
		err = DB.Transaction(apply)
	}
	if err != nil {
		return nil, err
	}
	if result.Applied {
		if err := cacheIncrUserQuota(userID, int64(quota)); err != nil {
			common.SysError(fmt.Sprintf("failed to sync quota credit for user %d: %s", userID, err))
		}
	}
	return &result, nil
}

func isQuotaCreditUniqueViolation(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var sqliteErr *sqlitedriver.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067
}
