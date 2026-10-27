package service

import (
	"context"
	"github.com/QuantumNous/new-api/internal/adapter/channels/codexmodels"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/discover_codex_models"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
)

func newCodexModelDiscovery(store channelstore.Store) discover_codex_models.Usecase {
	return discover_codex_models.NewUsecase(store, codexmodels.NewRemote(httpclient.NewProxyHttpClient, GetLatestCodexClientVersion, FetchCodexModels), func(ctx context.Context, id int) (*domain.CodexOAuthKey, error) {
		key, _, err := RefreshCodexChannelCredential(ctx, id, CodexCredentialRefreshOptions{ResetCaches: true})
		return key, err
	})
}
func FetchCodexChannelModels(channel *model.Channel) ([]string, error) {
	store := channelstore.NewStore()
	return newCodexModelDiscovery(store).Execute(store.Domain(channel))
}
