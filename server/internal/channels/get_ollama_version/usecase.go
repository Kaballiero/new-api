package get_ollama_version

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
}
type Provider interface {
	Version(string, string) (string, error)
}
type Usecase struct {
	store    Store
	provider Provider
}

func NewUsecase(store Store, provider Provider) Usecase {
	return Usecase{store: store, provider: provider}
}
func (usecase Usecase) Execute(input Input) (Output, error) {
	channel, err := usecase.store.Get(input.ID, true)
	if err != nil {
		return Output{}, domain.ErrOllamaNotFound
	}
	if channel.Type != constant.ChannelTypeOllama {
		return Output{}, domain.ErrNotOllamaChannel
	}
	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.BaseURLValue() != "" {
		baseURL = channel.BaseURLValue()
	}
	key := strings.Split(channel.Key, "\n")[0]
	version, err := usecase.provider.Version(baseURL, key)
	if err != nil {
		return Output{}, domain.OllamaVersionFailed(err)
	}
	return Output{Version: version}, nil
}
