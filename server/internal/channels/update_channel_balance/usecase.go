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
func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	channel, err := usecase.store.Cached(input.ID)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "internal", Cause: err}
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return Output{}, &domain.Failure{Kind: "message", Message: "Task Plugin channels do not support balance queries"}
	}
	if channel.ChannelInfo.IsMultiKey {
		return Output{}, &domain.Failure{Kind: "message", Message: "多密钥渠道不支持余额查询"}
	}
	result, err := usecase.reader.Read(channel)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "internal", Cause: err}
	}
	if result.RawResponse == "" {
		usecase.store.SaveBalance(channel, result.Balance)
	}
	return result, nil
}
