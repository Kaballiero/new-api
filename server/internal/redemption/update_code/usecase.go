package update_code

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Store reads and updates a code in separate persistence operations, without a shared transaction.
type Store interface {
	Get(id int) (domain.Code, error)
	Update(code domain.Code) error
}

type Input struct {
	ID            int
	Name          string
	Quota, Status int
	ExpiredTime   int64
	StatusOnly    bool
}

type Usecase struct {
	store Store
	// wallet validates the shared wallet limit before the update operation.
	wallet func(quota int) error
	// now reads the expiry cutoff outside a scenario transaction.
	now func() int64
}

func NewUsecase(store Store, wallet func(quota int) error, now func() int64) Usecase {
	return Usecase{store: store, wallet: wallet, now: now}
}

func (u Usecase) Execute(in Input) (domain.Code, error) {
	if in.ID == 0 {
		return domain.Code{}, domain.ErrIDEmpty
	}
	code, err := u.store.Get(in.ID)
	if err != nil {
		return domain.Code{}, err
	}
	if !in.StatusOnly {
		if err := domain.ValidateQuota(in.Quota, u.wallet); err != nil {
			return domain.Code{}, err
		}
		if in.ExpiredTime != 0 && in.ExpiredTime < u.now() {
			return domain.Code{}, domain.ErrExpiry
		}
		code.Name, code.Quota, code.ExpiredTime = in.Name, in.Quota, in.ExpiredTime
	} else {
		code.Status = in.Status
	}
	if err := domain.ValidateQuota(code.Quota, u.wallet); err != nil {
		return domain.Code{}, err
	}
	if err := u.store.Update(code); err != nil {
		return domain.Code{}, err
	}
	return code, nil
}
