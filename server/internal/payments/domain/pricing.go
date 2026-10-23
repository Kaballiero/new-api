package domain

import (
	"github.com/shopspring/decimal"
)

// PricingSettings reads money/display configuration at each calculation point.
type PricingSettings interface {
	TokenDisplay() bool
	QuotaPerUnit() float64
	GroupRatio(group string) float64
	Discount(amount int64) float64
	MinTopUp(provider string) int64
	UnitPrice(provider string) float64
}

// Capacity checks the current wallet; completion checks the ceiling again atomically.
type Capacity interface {
	Capacity(userID int, quota int) error
}

type Pricing struct {
	Settings PricingSettings
	Math     QuotaMath
	Wallet   Capacity
}

func (p Pricing) Credited(value decimal.Decimal) (int, error) {
	quota, err := p.Math.Strict(value)
	if err != nil {
		return 0, ErrQuotaRange
	}
	if quota <= 0 {
		return 0, ErrQuotaPositive
	}
	return quota, nil
}

func (p Pricing) Quota(amount int64) (int, error) {
	quota := decimal.NewFromInt(amount)
	unit := decimal.NewFromFloat(p.Settings.QuotaPerUnit())
	if p.Settings.TokenDisplay() {
		quota = decimal.NewFromInt(quota.Div(unit).IntPart()).Mul(unit)
	} else {
		quota = quota.Mul(unit)
	}
	return p.Math.Strict(quota)
}

func (p Pricing) MaximumAmount() int64 {
	if p.Settings.QuotaPerUnit() <= 0 {
		return 0
	}
	unit := decimal.NewFromFloat(p.Settings.QuotaPerUnit())
	maximum := decimal.NewFromInt(int64(p.Math.Maximum())).Div(unit).Floor()
	if p.Settings.TokenDisplay() {
		return maximum.Add(decimal.NewFromInt(1)).Mul(unit).Ceil().Sub(decimal.NewFromInt(1)).IntPart()
	}
	return maximum.IntPart()
}

func (p Pricing) Validate(amount int64) (int, error) {
	quota, err := p.Quota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maximum := p.MaximumAmount()
	if maximum > 0 && amount > maximum {
		return 0, MaximumAmount{Amount: maximum}
	}
	return 0, ErrAmount
}

func (p Pricing) Check(userID int, amount int64) error {
	quota, err := p.Validate(amount)
	if err != nil {
		return err
	}
	return p.Wallet.Capacity(userID, quota)
}

func (p Pricing) CheckCredited(userID int, value decimal.Decimal) error {
	quota, err := p.Credited(value)
	if err != nil {
		return err
	}
	return p.Wallet.Capacity(userID, quota)
}

func (p Pricing) Minimum(provider string) int64 {
	minimum := p.Settings.MinTopUp(provider)
	if !p.Settings.TokenDisplay() {
		return minimum
	}
	if provider == PaymentProviderStripe {
		return int64(int(minimum) * int(p.Settings.QuotaPerUnit()))
	}
	if provider != PaymentProviderEpay {
		return minimum
	}
	quota, err := p.Math.Strict(decimal.NewFromInt(minimum).Mul(decimal.NewFromFloat(p.Settings.QuotaPerUnit())))
	if err != nil {
		return int64(p.Math.Maximum())
	}
	return int64(quota)
}

func (p Pricing) Ratio(group string) float64 {
	ratio := p.Settings.GroupRatio(group)
	if ratio == 0 {
		return 1
	}
	return ratio
}

func (p Pricing) DecimalMoney(amount int64, group, provider string) float64 {
	value := decimal.NewFromInt(amount)
	if p.Settings.TokenDisplay() {
		value = value.Div(decimal.NewFromFloat(p.Settings.QuotaPerUnit()))
	}
	return value.Mul(decimal.NewFromFloat(p.Settings.UnitPrice(provider))).Mul(decimal.NewFromFloat(p.Ratio(group))).Mul(decimal.NewFromFloat(p.Discount(amount))).InexactFloat64()
}

func (p Pricing) FloatMoney(amount float64, group, provider string) float64 {
	original := amount
	if p.Settings.TokenDisplay() {
		amount /= p.Settings.QuotaPerUnit()
	}
	return amount * p.Settings.UnitPrice(provider) * p.Ratio(group) * p.Discount(int64(original))
}

func (p Pricing) StripeQuota(amount int64, group string) decimal.Decimal {
	return decimal.NewFromInt(amount).Mul(decimal.NewFromFloat(p.Ratio(group))).Mul(decimal.NewFromFloat(p.Settings.QuotaPerUnit()))
}

func (p Pricing) StoredAmount(amount int64) int64 {
	if p.Settings.TokenDisplay() {
		return decimal.NewFromInt(amount).Div(decimal.NewFromFloat(p.Settings.QuotaPerUnit())).IntPart()
	}
	return amount
}

func (p Pricing) Discount(amount int64) float64 {
	value := p.Settings.Discount(amount)
	if value > 0 {
		return value
	}
	return 1
}
