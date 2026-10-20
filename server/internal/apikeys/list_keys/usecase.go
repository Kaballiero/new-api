package list_keys

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	List(int, int, int) ([]*domain.Token, error)
	Count(int) (int64, error)
}

func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	ts, err := usecase.reader.List(input.UserID, input.Start, input.Limit)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "internal", Cause: err}
	}
	n, _ := usecase.reader.Count(input.UserID)
	return Output{ts, n}, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
