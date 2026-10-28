package get_billing_subscription

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Token(int) (*domain.BillingToken, error)
	UserQuota(int) (int, error)
	UsedQuota(int) (int, error)
}
type Rules struct {
	TokenStat func() bool
	Display   func() domain.QuotaDisplay
}
type Usecase struct {
	reader Reader
	rules  Rules
}

func NewUsecase(reader Reader, rules Rules) Usecase { return Usecase{reader: reader, rules: rules} }

func (usecase Usecase) Execute(caller domain.CompatibilityCaller) (Output, error) {
	var remainQuota, usedQuota int
	var err error
	var token *domain.BillingToken
	var expiredTime int64
	if usecase.rules.TokenStat() {
		token, err = usecase.reader.Token(caller.TokenID)
		expiredTime = token.ExpiredTime
		remainQuota = token.RemainQuota
		usedQuota = token.UsedQuota
	} else {
		remainQuota, err = usecase.reader.UserQuota(caller.UserID)
		usedQuota, err = usecase.reader.UsedQuota(caller.UserID)
	}
	if expiredTime <= 0 {
		expiredTime = 0
	}
	if err != nil {
		return Output{}, err
	}
	quota := remainQuota + usedQuota
	amount := usecase.rules.Display().Amount(float64(quota))
	if token != nil && token.UnlimitedQuota {
		amount = 100000000
	}
	return Output{Object: "billing_subscription", HasPaymentMethod: true, SoftLimitUSD: amount, HardLimitUSD: amount, SystemHardLimitUSD: amount, AccessUntil: expiredTime}, nil
}
