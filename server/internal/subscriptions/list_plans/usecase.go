package list_plans

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Reader executes the unpaginated plan query without changing its order.
type Reader interface {
	ListPlans(enabledOnly bool) ([]domain.SubscriptionPlan, error)
}
type Usecase struct {
	reader     Reader
	compliance func() bool
}

func NewUsecase(reader Reader, compliance func() bool) Usecase {
	return Usecase{reader: reader, compliance: compliance}
}
func (u Usecase) Execute() ([]domain.SubscriptionPlan, error) {
	if !u.compliance() {
		return []domain.SubscriptionPlan{}, nil
	}
	rows, err := u.reader.ListPlans(true)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].NormalizeDefaults()
	}
	return rows, nil
}
