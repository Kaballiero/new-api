package reveal_channel_key

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	IsMissing(error) bool
}
type Usecase struct {
	store Store
	audit func(int, string)
}

func NewUsecase(store Store, audit func(int, string)) Usecase {
	return Usecase{store: store, audit: audit}
}
func (usecase Usecase) Execute(input Input) (ChannelKeyResponse, error) {
	channel, err := usecase.store.Get(input.ID, true)
	if usecase.store.IsMissing(err) {
		return ChannelKeyResponse{}, domain.ErrChannelNotFound
	}
	if err != nil {
		return ChannelKeyResponse{}, err
	}
	if channel == nil {
		return ChannelKeyResponse{}, domain.ErrChannelNotFound
	}
	usecase.audit(input.ID, channel.Name)
	return ChannelKeyResponse{Key: channel.Key}, nil
}
