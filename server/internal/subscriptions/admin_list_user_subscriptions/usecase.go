package admin_list_user_subscriptions

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Reader lists every subscription of the selected user in the existing order.
type Reader interface {
	ListSubscriptions(userID int, activeOnly bool, now int64) ([]domain.UserSubscription, error)
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) Execute(userID int) ([]domain.UserSubscription, error) {
	if userID <= 0 {
		return nil, domain.ErrInvalidUserID
	}
	return u.reader.ListSubscriptions(userID, false, 0)
}
