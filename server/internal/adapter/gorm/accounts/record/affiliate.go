package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

type AffiliateTransfer struct {
	user *User
	db   func() *gorm.DB
	lock func(*gorm.DB) *gorm.DB
}

func (operation AffiliateTransfer) Begin() (transfer_affiliate_quota.AffiliateTransferTx, error) {
	tx := operation.db().Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return affiliateTransferTx{user: operation.user, tx: tx, lock: operation.lock}, nil
}

type affiliateTransferTx struct {
	lock func(*gorm.DB) *gorm.DB
	user *User
	tx   *gorm.DB
}

func (transaction affiliateTransferTx) LockBalances() (transfer_affiliate_quota.AffiliateBalances, error) {
	if err := transaction.lock(transaction.tx).First(transaction.user, transaction.user.Id).Error; err != nil {
		return transfer_affiliate_quota.AffiliateBalances{}, err
	}
	return transfer_affiliate_quota.AffiliateBalances{Quota: transaction.user.Quota, AffiliateQuota: transaction.user.AffQuota}, nil
}

func (transaction affiliateTransferTx) SaveBalances(balances transfer_affiliate_quota.AffiliateBalances) error {
	transaction.user.AffQuota = balances.AffiliateQuota
	transaction.user.Quota = balances.Quota
	return transaction.tx.Save(transaction.user).Error
}

func (transaction affiliateTransferTx) Commit() error   { return transaction.tx.Commit().Error }
func (transaction affiliateTransferTx) Rollback() error { return transaction.tx.Rollback().Error }

type AffiliateQuotaDisplay struct{}

func (AffiliateQuotaDisplay) MinimumQuota() float64        { return common.QuotaPerUnit }
func (AffiliateQuotaDisplay) FormatQuota(quota int) string { return logger.LogQuota(quota) }

func NewAffiliateTransfer(user *User, db func() *gorm.DB, lock func(*gorm.DB) *gorm.DB) AffiliateTransfer {
	return AffiliateTransfer{user: user, db: db, lock: lock}
}
