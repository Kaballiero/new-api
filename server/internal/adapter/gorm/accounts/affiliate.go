package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/model"
)

// AffiliateAccountLoader сохраняет запись аккаунта внутри адаптера до вызова перевода.
type AffiliateAccountLoader struct {
	SQL      core.Store
	Transfer func(*model.User, int) error
}

func (store AffiliateAccountLoader) LoadAffiliateAccount(id int) (transfer_affiliate_quota.AffiliateAccountTransfer, error) {
	user, err := store.SQL.GetUserById(id, true)
	if err != nil {
		return nil, err
	}
	return affiliateAccountTransfer{user: user, transfer: store.Transfer}, nil
}

type affiliateAccountTransfer struct {
	user     *model.User
	transfer func(*model.User, int) error
}

func (account affiliateAccountTransfer) TransferQuota(quota int) error {
	return account.transfer(account.user, quota)
}
