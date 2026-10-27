package channelcore

import (
	"context"

	codex "github.com/QuantumNous/new-api/internal/adapter/channels/codex"
	"github.com/QuantumNous/new-api/internal/adapter/channels/codexmodels"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/discover_codex_models"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	legacy "github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
)

func newCodexModelDiscovery(store channelstore.Store) discover_codex_models.Usecase {
	return discover_codex_models.NewUsecase(
		store,
		codexmodels.NewRemote(
			httpclient.NewProxyHttpClient,
			codex.GetLatestCodexClientVersion,
			codex.FetchCodexModels,
		),
		func(
			ctx context.Context,
			id int,
		) (*domain.CodexOAuthKey, error) {
			key, _, err := RefreshCodexChannelCredential(ctx, id, CodexCredentialRefreshOptions{ResetCaches: true})
			return key, err
		},
	)
}
func FetchCodexChannelModels(channel *legacy.Channel) ([]string, error) {
	store := channelstore.NewStore()
	return newCodexModelDiscovery(store).Execute(store.Domain(channel))
}
