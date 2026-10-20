package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/model"
)

// AffiliateAccountLoader сохраняет запись аккаунта внутри адаптера до вызова перевода.
type AffiliateAccountLoader struct{}

func (AffiliateAccountLoader) LoadAffiliateAccount(id int) (transfer_affiliate_quota.AffiliateAccountTransfer, error) {
	user, err := model.GetUserById(id, true)
	if err != nil {
		return nil, err
	}
	return affiliateAccountTransfer{user: user}, nil
}

type affiliateAccountTransfer struct{ user *model.User }

func (account affiliateAccountTransfer) TransferQuota(quota int) error {
	return account.user.TransferAffQuotaToQuota(quota)
}
