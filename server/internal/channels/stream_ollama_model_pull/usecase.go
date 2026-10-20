package stream_ollama_model_pull

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
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
func (usecase Usecase) Execute(req Input) *domain.Failure {
	if req.ChannelID == 0 || req.ModelName == "" {
		return &domain.Failure{Kind: "invalid", Message: "Channel ID and model name are required"}
	}

	// 获取渠道信息
	channel, err := usecase.store.Get(req.ChannelID, true)
	if err != nil {
		return &domain.Failure{Kind: "missing", Message: "Channel not found"}
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		return &domain.Failure{Kind: "invalid", Message: "This operation is only supported for Ollama channels"}
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.BaseURLValue() != "" {
		baseURL = channel.BaseURLValue()
	}

	usecase.start()
	key := strings.Split(channel.Key, "\n")[0]
	if err := usecase.provider.Stream(baseURL, key, req.ModelName, usecase.progress); err != nil {
		return &domain.Failure{Kind: "stream", Cause: err}
	}
	return nil
}
