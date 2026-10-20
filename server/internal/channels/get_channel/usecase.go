package get_channel

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	IsMissing(error) bool
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute(input Input) (*Output, *domain.Failure) {
	channel, err := usecase.store.Get(input.ID, false)
	if usecase.store.IsMissing(err) {
		return nil, &domain.Failure{Kind: "missing"}
	}
	if err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	if channel == nil {
		return nil, &domain.Failure{Kind: "missing"}
	}
	channel.HideMultiKeyFailures()
	return channel, nil
}
