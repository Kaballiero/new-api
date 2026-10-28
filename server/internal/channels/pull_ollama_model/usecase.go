package pull_ollama_model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
}
type Provider interface {
	Pull(string, string, string) error
}
type Usecase struct {
	store    Store
	provider Provider
}

func NewUsecase(store Store, provider Provider) Usecase {
	return Usecase{store: store, provider: provider}
}
func (usecase Usecase) Execute(req Input) (Output, error) {
	if req.ChannelID == 0 || req.ModelName == "" {
		return Output{}, domain.ErrOllamaInputRequired
	}
	channel, err := usecase.store.Get(req.ChannelID, true)
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
	err = usecase.provider.Pull(baseURL, key, req.ModelName)
	if err != nil {
		return Output{}, fmt.Errorf("%w: %s", domain.ErrOllamaPullFailed, err.Error())
	}
	return Output{Message: fmt.Sprintf("Model %s pulled successfully", req.ModelName)}, nil
}
