package stream_ollama_model_pull

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
}
type Provider interface {
	Stream(string, string, string, func(domain.PullProgress)) error
}
type Usecase struct {
	store    Store
	provider Provider
	start    func()
	progress func(domain.PullProgress)
}

func NewUsecase(store Store, provider Provider, start func(), progress func(domain.PullProgress)) Usecase {
	return Usecase{store: store, provider: provider, start: start, progress: progress}
}

// Execute returns an ErrOllamaStreamFailed error once the stream has started.
func (usecase Usecase) Execute(req Input) error {
	if req.ChannelID == 0 || req.ModelName == "" {
		return domain.ErrOllamaInputRequired
	}
	channel, err := usecase.store.Get(req.ChannelID, true)
	if err != nil {
		return domain.ErrOllamaNotFound
	}
	if channel.Type != constant.ChannelTypeOllama {
		return domain.ErrNotOllamaChannel
	}
	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.BaseURLValue() != "" {
		baseURL = channel.BaseURLValue()
	}
	usecase.start()
	key := strings.Split(channel.Key, "\n")[0]
	if err := usecase.provider.Stream(baseURL, key, req.ModelName, usecase.progress); err != nil {
		return domain.OllamaStreamFailed(err)
	}
	return nil
}
