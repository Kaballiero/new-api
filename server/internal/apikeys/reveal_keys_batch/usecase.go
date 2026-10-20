package reveal_keys_batch

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	KeysBatch([]int, int) ([]domain.Token, error)
}

func (usecase Usecase) Execute(ids []int, userID int) (Output, *domain.Failure) {
	params := usecase.audit(ids)
	if len(ids) == 0 {
		return Output{}, &domain.Failure{Kind: "invalid"}
	}
	if len(ids) > 100 {
		return Output{}, &domain.Failure{Kind: "batch_many", Params: map[string]any{"Max": 100}}
	}
	ts, err := usecase.reader.KeysBatch(ids, userID)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "plain", Cause: err}
	}
	keys := make(map[int]string)
	returned := make([]int, 0, len(ts))
	for _, t := range ts {
		keys[t.Id] = t.Key
		returned = append(returned, t.Id)
	}
	params["count"] = len(ts)
	params["returned_ids"] = returned
	usecase.succeed()
	return Output{Count: len(ts), Keys: keys}, nil
}

type Usecase struct {
	reader  Reader
	audit   func([]int) map[string]any
	succeed func()
}

func NewUsecase(reader Reader, audit func([]int) map[string]any, succeed func()) Usecase {
	return Usecase{reader: reader, audit: audit, succeed: succeed}
}
