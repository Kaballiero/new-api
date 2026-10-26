package update_prefill_group

import "github.com/QuantumNous/new-api/internal/catalog/domain/prefill"

type Store interface {
	Duplicate(int, string) (bool, error)
	Save(*prefill.Group) error
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store: store, now: now} }
func (u Usecase) Execute(value prefill.Group) (prefill.Group, error) {
	if value.Id == 0 {
		return value, prefill.ErrID
	}
	if duplicate, err := u.store.Duplicate(value.Id, value.Name); err != nil {
		return value, err
	} else if duplicate {
		return value, prefill.ErrDuplicate
	}
	now := u.now()
	value.UpdatedTime = now
	err := u.store.Save(&value)
	return value, err
}
