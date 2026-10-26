package create_prefill_group

import "github.com/QuantumNous/new-api/internal/catalog/domain/prefill"

type Store interface {
	Duplicate(int, string) (bool, error)
	Insert(*prefill.Group) error
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store: store, now: now} }
func (u Usecase) Execute(value prefill.Group) (prefill.Group, error) {
	if value.Name == "" || value.Type == "" {
		return value, prefill.ErrNameAndType
	}
	if duplicate, err := u.store.Duplicate(0, value.Name); err != nil {
		return value, err
	} else if duplicate {
		return value, prefill.ErrDuplicate
	}
	now := u.now()
	value.UpdatedTime = now
	value.CreatedTime = now
	err := u.store.Insert(&value)
	return value, err
}
