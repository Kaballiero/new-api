package list_missing_models

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Store interface {
	EnabledModels() []string
	ExistingNames([]string) ([]string, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (u Usecase) Execute() (Output, error) {
	enabled := u.store.EnabledModels()
	if len(enabled) == 0 {
		return []string{}, nil
	}
	existing, err := u.store.ExistingNames(enabled)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return domain.MissingNames(enabled, existing), nil
}
