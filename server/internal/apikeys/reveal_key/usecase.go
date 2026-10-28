package reveal_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Reader interface {
	Get(int, int) (*domain.Token, error)
	IsMissing(error) bool
}

func (usecase Usecase) Execute(input Input) (*domain.Token, error) {
	t, err := usecase.reader.Get(input.ID, input.UserID)
	if usecase.reader.IsMissing(err) {
		return nil, domain.ErrKeyNotFound
	}
	if err != nil {
		return nil, domain.Internal(err)
	}
	params := usecase.audit()
	params["id"], params["name"] = t.Id, t.Name
	usecase.succeed()
	return t, nil
}

type Usecase struct {
	reader  Reader
	audit   func() map[string]any
	succeed func()
}

func NewUsecase(reader Reader, audit func() map[string]any, succeed func()) Usecase {
	return Usecase{reader: reader, audit: audit, succeed: succeed}
}
