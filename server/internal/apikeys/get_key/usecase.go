package get_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Get(int, int) (*domain.Token, error)
	IsMissing(error) bool
}

func (usecase Usecase) Execute(input Input) (*domain.Token, *domain.Failure) {
	t, err := usecase.reader.Get(input.ID, input.UserID)
	if usecase.reader.IsMissing(err) {
		return nil, &domain.Failure{Kind: "missing"}
	}
	if err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	return t, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
