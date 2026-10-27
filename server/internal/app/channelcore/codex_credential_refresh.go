package channelcore

import (
	"context"
	"time"

	codex "github.com/QuantumNous/new-api/internal/adapter/channels/codex"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/refresh_codex_credential"
	legacy "github.com/QuantumNous/new-api/model"
)

type CodexCredentialRefreshOptions struct {
	ResetCaches bool
}

func RefreshCodexChannelCredential(
	ctx context.Context,
	channelID int,
	opts CodexCredentialRefreshOptions,
) (*channeldomain.CodexOAuthKey, *legacy.Channel, error) {
	scenario := refresh_codex_credential.NewUsecase(
		channelstore.NewStore(),
		func(
			ctx context.Context,
			token, proxy string,
		) (*channeldomain.CodexTokenResult, error) {
			result, err := codex.RefreshCodexOAuthTokenWithProxy(ctx, token, proxy)
			if result == nil {
				return nil, err
			}
			return &channeldomain.CodexTokenResult{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: result.ExpiresAt}, err
		},
		codex.ExtractCodexAccountIDFromJWT,
		codex.ExtractEmailFromJWT,
		time.Now,
	)
	key, channel, err := scenario.Execute(ctx, refresh_codex_credential.Input{ChannelID: channelID, ResetCaches: opts.ResetCaches})
	return key, channelstore.FromDomain(channel), err
}
