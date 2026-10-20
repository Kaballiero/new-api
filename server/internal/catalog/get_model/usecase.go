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
func (u Usecase) Execute(input Input) (*Output, *domain.Failure) {
	value, err := u.store.Model(input.ID)
	if err != nil {
		if u.store.IsMissing(err) {
			return nil, &domain.Failure{Kind: "model_not_found", Message: "model not found"}
		}
		return nil, &domain.Failure{Kind: "internal_error", Cause: err}
	}
	if len([]*domain.Model{value}) > 0 {
		configured, err := u.store.ConfiguredChannels()
		if err != nil {
			u.log("load configured model channels: " + err.Error())
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		}
		domain.FillConfiguredCounts([]*domain.Model{value}, configured)
		connections, err := u.store.Connections()
		if err != nil {
			u.log("load model connections: " + err.Error())
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		}
		metadata, err := u.store.AllModels()
		if err != nil {
			u.log("load model square states: " + err.Error())
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		}
		if err := domain.FillModelSquareStates([]*domain.Model{value}, configured, connections, metadata); err != nil {
			u.log("load model square states: " + err.Error())
			return nil, &domain.Failure{Kind: "internal_error", Cause: err}
		}
		domain.FillConnections([]*domain.Model{value}, connections, u.store.SupportEndpoints, u.store.QuotaTypes)
	}
	return value, nil
}
