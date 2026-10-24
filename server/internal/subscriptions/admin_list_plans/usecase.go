package admin_list_plans

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Reader executes the unpaginated plan query without changing its order.
type Reader interface {
	ListPlans(enabledOnly bool) ([]domain.SubscriptionPlan, error)
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) Execute() ([]domain.SubscriptionPlan, error) {
	rows, err := u.reader.ListPlans(false)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].NormalizeDefaults()
	}
	return rows, nil
}
