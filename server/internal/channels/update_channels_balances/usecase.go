package update_channels_balances

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	All() ([]*domain.Channel, error)
	SaveBalance(*domain.Channel, float64)
}
type Reader interface {
	Read(*domain.Channel) (domain.BalanceResult, error)
}
type Usecase struct {
	store  Store
	reader Reader
	// disable turns off a channel whose balance is exhausted.
	disable func(*domain.Channel)
	pause   func()
}

func NewUsecase(store Store, reader Reader, disable func(*domain.Channel), pause func()) Usecase {
	return Usecase{store: store, reader: reader, disable: disable, pause: pause}
}
func (usecase Usecase) Execute() error {
	channels, err := usecase.store.All()
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.Status != 1 || channel.ChannelInfo.IsMultiKey {
			continue
		}
		result, err := usecase.reader.Read(channel)
		if err != nil {
			continue
		}
		if result.RawResponse == "" {
			usecase.store.SaveBalance(channel, result.Balance)
			if result.Balance <= 0 {
				usecase.disable(channel)
			}
		}
		usecase.pause()
	}
	return nil
}
