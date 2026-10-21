package update_channel_balance

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Cached(int) (*domain.Channel, error)
	SaveBalance(*domain.Channel, float64)
}
type Reader interface {
	Read(*domain.Channel) (domain.BalanceResult, error)
}
type Usecase struct {
	store  Store
	reader Reader
}

func NewUsecase(store Store, reader Reader) Usecase { return Usecase{store: store, reader: reader} }

// Execute returns cache and upstream failures as they are: they are answered
// with their text and HTTP 200.
func (usecase Usecase) Execute(input Input) (Output, error) {
	channel, err := usecase.store.Cached(input.ID)
	if err != nil {
		return Output{}, err
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return Output{}, domain.ErrBalanceTaskPlugin
	}
	if channel.ChannelInfo.IsMultiKey {
		return Output{}, domain.ErrBalanceMultiKey
	}
	result, err := usecase.reader.Read(channel)
	if err != nil {
		return Output{}, err
	}
	if result.RawResponse == "" {
		usecase.store.SaveBalance(channel, result.Balance)
	}
	return result, nil
}
