package quote_waffo_topup

import (
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	UserID int
	Amount int64
}

type Usecase struct {
	pricing domain.Pricing
	users   domain.Users
}

func NewUsecase(pricing domain.Pricing, users domain.Users) Usecase {
	return Usecase{pricing: pricing, users: users}
}

func (u Usecase) Execute(in Input) (float64, error) {
	if in.Amount < u.pricing.Minimum(domain.PaymentProviderWaffo) {
		return 0, domain.MinimumAmount{Amount: u.pricing.Minimum(domain.PaymentProviderWaffo)}
	}
	if err := u.pricing.Check(in.UserID, in.Amount); err != nil {
		return 0, err
	}
	group, err := u.users.Group(in.UserID)
	if err != nil {
		return 0, domain.ErrGroup
	}
	money := u.pricing.FloatMoney(float64(in.Amount), group, domain.PaymentProviderWaffo)
	if money <= 0.01 {
		return 0, domain.ErrLowAmount
	}
	return money, nil
}
