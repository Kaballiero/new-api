package delete_keys_batch

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface{ DeleteBatch([]int, int) (int, error) }

func (usecase Usecase) Execute(ids []int, userID int) (Output, error) {
	params := usecase.audit(ids)
	if len(ids) == 0 {
		return Output{}, domain.ErrBatchEmpty
	}
	n, err := usecase.store.DeleteBatch(ids, userID)
	if err != nil {
		return Output{}, err
	}
	params["count"] = n
	usecase.succeed()
	return Output{Count: n}, nil
}

type Usecase struct {
	store   Store
	audit   func([]int) map[string]any
	succeed func()
}

func NewUsecase(store Store, audit func([]int) map[string]any, succeed func()) Usecase {
	return Usecase{store: store, audit: audit, succeed: succeed}
}
