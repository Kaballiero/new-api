package get_billing_usage

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Token(int) (*domain.BillingToken, error)
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
	var quota int
	var err error
	if usecase.rules.TokenStat() {
		token, lookupErr := usecase.reader.Token(caller.TokenID)
		err = lookupErr
		quota = token.UsedQuota
	} else {
		quota, err = usecase.reader.UsedQuota(caller.UserID)
	}
	if err != nil {
		return Output{}, err
	}
	amount := usecase.rules.Display().Amount(float64(quota))
	return Output{Object: "list", TotalUsage: amount * 100}, nil
}
