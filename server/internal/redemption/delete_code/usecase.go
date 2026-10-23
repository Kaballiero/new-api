package delete_code

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Store loads and deletes a code in separate persistence operations, without a shared transaction.
type Store interface {
	GetForDelete(id int) (domain.Code, error)
	Delete(code domain.Code) error
}

type Usecase struct {
	store Store
}

func NewUsecase(store Store) Usecase {
	return Usecase{store: store}
}

func (u Usecase) Execute(id int) error {
	if id == 0 {
		return domain.ErrIDEmpty
	}
	code, err := u.store.GetForDelete(id)
	if err != nil {
		return err
	}
	return u.store.Delete(code)
}
