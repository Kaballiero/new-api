package quote_stripe_topup

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
	if in.Amount < u.pricing.Minimum(domain.PaymentProviderStripe) {
		return 0, domain.MinimumAmount{Amount: u.pricing.Minimum(domain.PaymentProviderStripe)}
	}
	if in.Amount > 10000 {
		return 0, domain.ErrStripeMaximum
	}
	group, err := u.users.Group(in.UserID)
	if err != nil {
		return 0, domain.ErrGroup
	}
	if err := u.pricing.CheckCredited(in.UserID, u.pricing.StripeQuota(in.Amount, group)); err != nil {
		return 0, err
	}
	money := u.pricing.FloatMoney(float64(in.Amount), group, domain.PaymentProviderStripe)
	if money <= 0.01 {
		return 0, domain.ErrLowAmount
	}
	return money, nil
}
