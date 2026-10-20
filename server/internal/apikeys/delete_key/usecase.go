package delete_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	Get(int, int) (*domain.Token, error)
	IsMissing(error) bool
	Delete(*domain.Token) error
}

func (usecase Usecase) Execute(input Input) (*domain.Token, *domain.Failure) {
	t, err := usecase.store.Get(input.ID, input.UserID)
	if usecase.store.IsMissing(err) {
		return nil, &domain.Failure{Kind: "missing"}
	}
	if err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	params := usecase.audit()
	params["id"], params["name"] = t.Id, t.Name
	if err := usecase.store.Delete(t); err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	usecase.succeed()
	return t, nil
}

type Usecase struct {
	store   Store
	audit   func() map[string]any
	succeed func()
}

func NewUsecase(store Store, audit func() map[string]any, succeed func()) Usecase {
	return Usecase{store: store, audit: audit, succeed: succeed}
}
