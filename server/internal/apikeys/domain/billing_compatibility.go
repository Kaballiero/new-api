package domain

// BillingToken is the existing API-key accounting projection used by compatibility reads.
type BillingToken struct {
	ExpiredTime    int64
	RemainQuota    int
	UsedQuota      int
	UnlimitedQuota bool
}

type CompatibilityCaller struct{ UserID, TokenID int }

// QuotaDisplay retains the site's presentation units without changing stored quota.
type QuotaDisplay struct {
	Tokens, CNY           bool
	PerUnit, ExchangeRate float64
}

func (display QuotaDisplay) Amount(amount float64) float64 {
	if display.Tokens {
		return amount
	}
	if display.CNY {
		return amount / display.PerUnit * display.ExchangeRate
	}
	return amount / display.PerUnit
}
