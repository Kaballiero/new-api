package update_prefill_group

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Store interface {
	Duplicate(int, string) (bool, error)
	Save(*domain.PrefillGroup) error
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store: store, now: now} }
func (u Usecase) Execute(value domain.PrefillGroup) (domain.PrefillGroup, error) {
	if value.Id == 0 {
		return value, domain.ErrPrefillID
	}
	if duplicate, err := u.store.Duplicate(value.Id, value.Name); err != nil {
		return value, err
	} else if duplicate {
		return value, domain.ErrPrefillDuplicate
	}
	now := u.now()
	value.UpdatedTime = now
	err := u.store.Save(&value)
	return value, err
}
