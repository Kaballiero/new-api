package fetch_upstream_models

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	IsMissing(error) bool
}
type Discovery interface {
	Fetch(*domain.Channel) ([]string, error)
}
type Usecase struct {
	store     Store
	discovery Discovery
}

func NewUsecase(store Store, discovery Discovery) Usecase {
	return Usecase{store: store, discovery: discovery}
}
func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	channel, err := usecase.store.Get(input.ID, true)
	if usecase.store.IsMissing(err) {
		return nil, &domain.Failure{Kind: "missing"}
	}
	if err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	models, err := usecase.discovery.Fetch(channel)
	if err != nil {
		return nil, &domain.Failure{Kind: "fetch", Message: fmt.Sprintf("获取模型列表失败: %s", err.Error())}
	}
	return models, nil
}
