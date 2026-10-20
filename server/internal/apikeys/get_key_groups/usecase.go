package get_key_groups

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Group() (string, error)
	Options(string) []string
	Max() int
}

func (usecase Usecase) Execute() (Output, *domain.Failure) {
	g, e := usecase.reader.Group()
	if e != nil {
		return Output{}, &domain.Failure{Kind: "plain", Cause: e}
	}
	return Output{usecase.reader.Options(g), usecase.reader.Max()}, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
