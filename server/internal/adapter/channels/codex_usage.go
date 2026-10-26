package channels

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/service"
	"net/http"
)

type CodexUsage struct {
	fetch func(context.Context, *http.Client, string, string, string) (int, []byte, error)
}

func NewCodexUsage(fetch func(context.Context, *http.Client, string, string, string) (int, []byte, error)) CodexUsage {
	return CodexUsage{fetch: fetch}
}
func (provider CodexUsage) Open(proxy string) (domain.CodexUsageFetch, error) {
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, base, token, account string) (int, []byte, error) {
		return provider.fetch(ctx, client, base, token, account)
	}, nil
}
func (provider CodexUsage) Refresh(ctx context.Context, token, proxy string) (*domain.CodexTokenResult, error) {
	result, err := service.RefreshCodexOAuthTokenWithProxy(ctx, token, proxy)
	if result == nil {
		return nil, err
	}
	return &domain.CodexTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: result.ExpiresAt}, err
}

func (CodexUsage) DecodeUsage(body []byte) (any, error) {
	var payload any
	err := common.Unmarshal(body, &payload)
	return payload, err
}
