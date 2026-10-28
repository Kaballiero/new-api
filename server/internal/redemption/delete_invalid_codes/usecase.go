package delete_invalid_codes

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Store removes matching invalid codes in one persistence operation, without a scenario transaction.
type Store interface {
	DeleteInvalid(filter domain.InvalidCodes) (int64, error)
}

type Usecase struct {
	store Store
	// now captures the expiry cutoff before the delete operation.
	now func() int64
}

func NewUsecase(store Store, now func() int64) Usecase {
	return Usecase{store: store, now: now}
}

func (u Usecase) Execute() (int64, error) {
	return u.store.DeleteInvalid(domain.InvalidCodes{Statuses: []int{domain.StatusUsed, domain.StatusDisabled},
		ExpiringStatus: domain.StatusEnabled, Before: u.now()})
}
