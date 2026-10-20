package get_ollama_version

import (
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
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
func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	channel, err := usecase.store.Get(input.ID, true)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "missing", Message: "Channel not found"}
	}

	if channel.Type != constant.ChannelTypeOllama {
		return Output{}, &domain.Failure{Kind: "invalid", Message: "This operation is only supported for Ollama channels"}
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.BaseURLValue() != "" {
		baseURL = channel.BaseURLValue()
	}

	key := strings.Split(channel.Key, "\n")[0]
	version, err := usecase.provider.Version(baseURL, key)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "provider", Message: fmt.Sprintf("获取Ollama版本失败: %s", err.Error())}
	}

	return Output{Version: version}, nil
}
