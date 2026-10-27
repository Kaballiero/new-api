package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

func ViolationFeeQuota(amount, groupRatio float64) int {
	if amount <= 0 {
		return 0
	}
	if groupRatio <= 0 {
		return 0
	}
	quota := common.QuotaFromDecimal(decimal.NewFromFloat(amount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(groupRatio)).
		Round(0))
	if quota <= 0 {
		return 0
	}
	return quota
}
