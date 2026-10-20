package enrich_models

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Store interface {
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
func (u Usecase) Execute(items []*domain.Model) *domain.Failure {
	if len(items) > 0 {
		configured, err := u.store.ConfiguredChannels()
		if err != nil {
			u.log("load configured model channels: " + err.Error())
			return &domain.Failure{Kind: "internal_error", Cause: err}
		}
		domain.FillConfiguredCounts(items, configured)
		connections, err := u.store.Connections()
		if err != nil {
			u.log("load model connections: " + err.Error())
			return &domain.Failure{Kind: "internal_error", Cause: err}
		}
		metadata, err := u.store.AllModels()
		if err != nil {
			u.log("load model square states: " + err.Error())
			return &domain.Failure{Kind: "internal_error", Cause: err}
		}
		if err := domain.FillModelSquareStates(items, configured, connections, metadata); err != nil {
			u.log("load model square states: " + err.Error())
			return &domain.Failure{Kind: "internal_error", Cause: err}
		}
		domain.FillConnections(items, connections, u.store.SupportEndpoints, u.store.QuotaTypes)
	}
	return nil
}
