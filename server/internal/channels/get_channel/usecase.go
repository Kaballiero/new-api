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
func (usecase Usecase) Execute(input Input) (*Output, error) {
	channel, err := usecase.store.Get(input.ID, false)
	if usecase.store.IsMissing(err) {
		return nil, domain.ErrChannelNotFound
	}
	if err != nil {
		return nil, domain.Internal(err)
	}
	if channel == nil {
		return nil, domain.ErrChannelNotFound
	}
	channel.HideMultiKeyFailures()
	return channel, nil
}
