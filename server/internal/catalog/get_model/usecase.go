package get_model

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
	Model(int) (*domain.Model, error)
	IsMissing(error) bool
	ConfiguredChannels() (map[string][]int, error)
	Connections() ([]domain.ModelConnection, error)
	AllModels() ([]domain.Model, error)
	SupportEndpoints(string) []string
	QuotaTypes(string) []int
}
type Usecase struct {
	store Store
	log   func(string)
}

func NewUsecase(store Store, log func(string)) Usecase { return Usecase{store: store, log: log} }
func (u Usecase) Execute(input Input) (*Output, error) {
	value, err := u.store.Model(input.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return nil, domain.ErrModelNotFound
		}
		return nil, domain.Internal(err)
	}
	if err := domain.EnrichModels([]*domain.Model{value}, u.store, u.log); err != nil {
		return nil, err
	}
	return value, nil
}
