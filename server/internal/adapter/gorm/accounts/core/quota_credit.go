package core

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/credit_getapi_quota"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func (store Store) CreditTransaction(run func(credit_getapi_quota.Transaction) error) error {
	return store.env.DB().Transaction(func(tx *gorm.DB) error {
		// SQLite must acquire a write lock before the first receipt read.
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Model(&record.GetAPIQuotaCredit{}).Where("1 = 0").Update("quota", gorm.Expr("quota")).Error; err != nil {
				return err
			}
		}
		return run(creditTransaction{tx: tx})
	})
}
func (Store) RetryCredit(err error) bool {
	return isQuotaCreditUniqueViolation(err) || errors.Is(err, gorm.ErrRecordNotFound)
}

type creditTransaction struct{ tx *gorm.DB }

func (t creditTransaction) Receipt(key string) (*credit_getapi_quota.Receipt, error) {
	var receipt record.GetAPIQuotaCredit
	err := t.tx.Where("operation_key = ?", []byte(key)).First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &credit_getapi_quota.Receipt{UserID: receipt.UserID, Quota: receipt.Quota}, nil
}
func (t creditTransaction) Balance(userID int, replay bool) (int, error) {
	var user record.User
	tx := locking.LockForUpdate(t.tx)
	if replay {
		tx = t.tx.Unscoped()
	}
	err := tx.First(&user, userID).Error
	return user.Quota, err
}
func (t creditTransaction) InsertReceipt(userID int, key string, quota int, createdAt int64) error {
	return t.tx.Create(&record.GetAPIQuotaCredit{OperationKey: []byte(key), UserID: userID, Quota: quota, CreatedAt: createdAt}).Error
}
func (t creditTransaction) UpdateQuota(userID, quota int) error {
	update := t.tx.Model(&record.User{}).Where("id = ?", userID).Update("quota", quota)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
