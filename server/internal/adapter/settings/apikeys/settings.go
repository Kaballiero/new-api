package apikeys

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

func MaxQuota() int {
	quota, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(1_000_000_000).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		return common.MaxWalletQuota
	}
	return quota
}
