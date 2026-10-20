package pull_ollama_model

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
	Pull(string, string, string) error
}
type Usecase struct {
	store    Store
	provider Provider
}

func NewUsecase(store Store, provider Provider) Usecase {
	return Usecase{store: store, provider: provider}
}
func (usecase Usecase) Execute(req Input) (Output, *domain.Failure) {
	if req.ChannelID == 0 || req.ModelName == "" {
		return Output{}, &domain.Failure{Kind: "invalid", Message: "Channel ID and model name are required"}
	}

	// 获取渠道信息
	channel, err := usecase.store.Get(req.ChannelID, true)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "missing", Message: "Channel not found"}
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		return Output{}, &domain.Failure{Kind: "invalid", Message: "This operation is only supported for Ollama channels"}
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.BaseURLValue() != "" {
		baseURL = channel.BaseURLValue()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = usecase.provider.Pull(baseURL, key, req.ModelName)
	if err != nil {
		return Output{}, &domain.Failure{Kind: "provider", Message: fmt.Sprintf("Failed to pull model: %s", err.Error())}
	}

	return Output{Message: fmt.Sprintf("Model %s pulled successfully", req.ModelName)}, nil

}
