package get_own_subscriptions

import "github.com/QuantumNous/new-api/internal/subscriptions/domain"

// Reader keeps settings, all subscriptions and active subscriptions as independent reads.
type Reader interface {
	BillingPreference(userID int) (string, error)
	ListSubscriptions(userID int, activeOnly bool, now int64) ([]domain.UserSubscription, error)
}
type Result struct {
	Preference  string
	All, Active []domain.UserSubscription
}
type Usecase struct {
	reader    Reader
	normalize func(string) string
	now       func() int64
}

func NewUsecase(reader Reader, normalize func(string) string, now func() int64) Usecase {
	return Usecase{reader: reader, normalize: normalize, now: now}
}
func (u Usecase) Execute(userID int) (Result, error) {
	preference, _ := u.reader.BillingPreference(userID)
	result := Result{Preference: u.normalize(preference)}
	// Legacy self intentionally projects a failed individual read as an empty list.
	var err error
	result.All, err = u.reader.ListSubscriptions(userID, false, 0)
	if err != nil {
		result.All = []domain.UserSubscription{}
	}
	result.Active, err = u.reader.ListSubscriptions(userID, true, u.now())
	if err != nil {
		result.Active = []domain.UserSubscription{}
	}
	return result, nil
}
