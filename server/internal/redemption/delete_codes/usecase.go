package delete_codes

import "github.com/QuantumNous/new-api/internal/redemption/domain"

// Store soft-deletes the requested codes in one persistence operation, without a scenario transaction.
type Store interface {
	DeleteIDs(ids []int) (int64, error)
}

type Usecase struct {
	store Store
}

func NewUsecase(store Store) Usecase {
	return Usecase{store: store}
}

func (u Usecase) Execute(ids []int) (int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return 0, domain.ErrBatchCount
	}
	for _, id := range ids {
		if id <= 0 {
			return 0, domain.ErrBatchID
		}
	}
	return u.store.DeleteIDs(ids)
}
